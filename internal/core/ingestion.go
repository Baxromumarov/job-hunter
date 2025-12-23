package core

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/baxromumarov/job-hunter/internal/ai"
	"github.com/baxromumarov/job-hunter/internal/scraper"
	"github.com/baxromumarov/job-hunter/internal/store"
)

type IngestionService struct {
	store      *store.Store
	matcher    *MatcherService
	normalizer scraper.Normalizer
	keywords   []string
	profile    ai.CandidateProfile
}

func NewIngestionService(store *store.Store, matcher *MatcherService) *IngestionService {
	return &IngestionService{
		store:      store,
		matcher:    matcher,
		normalizer: scraper.NewSimpleNormalizer(),
		keywords:   []string{"golang", "go developer", "backend", "microservices", "grpc", "distributed systems", "software engineer"},
		profile: ai.CandidateProfile{
			TechStack: []string{"golang", "backend", "grpc", "rest", "postgresql", "redis", "docker", "linux"},
		},
	}
}

func (s *IngestionService) Start(ctx context.Context) {
	go s.scrapeLoop(ctx, 30*time.Minute)
	go s.cleanupLoop(ctx, 24*time.Hour, 30*24*time.Hour)
}

func (s *IngestionService) scrapeLoop(ctx context.Context, interval time.Duration) {
	s.scrapeOnce(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scrapeOnce(ctx)
		}
	}
}

func (s *IngestionService) scrapeOnce(ctx context.Context) {
	sources, err := s.store.ListSources(ctx, 200, 0)
	if err != nil {
		log.Printf("Ingestion: failed to list sources: %v", err)
		return
	}

	since := time.Now().Add(-10 * 24 * time.Hour)

	for _, src := range sources {
		select {
		case <-ctx.Done():
			return
		default:
		}

		scr := scraper.NewGenericScraper(src.URL)
		rawJobs, err := scr.FetchJobs(since)
		if err != nil {
			log.Printf("Ingestion: scrape failed for %s: %v", src.URL, err)
			continue
		}

		for _, raw := range rawJobs {
			postDate := raw.PostedAt
			if postDate.IsZero() {
				postDate = time.Now()
			}
			if postDate.Before(since) {
				continue
			}

			desc := raw.Description
			if normalized, err := s.normalizer.Normalize(raw.Description); err == nil && normalized != "" {
				desc = normalized
			}

			finalScore, summary := s.scoreJob(ctx, raw.Title, desc)
			if finalScore < 70 {
				continue
			}

			job := store.Job{
				SourceID:     src.ID,
				SourceURL:    src.URL,
				SourceType:   src.Type,
				URL:          raw.URL,
				Title:        raw.Title,
				Description:  desc,
				Company:      raw.Company,
				Location:     raw.Location,
				MatchScore:   finalScore,
				MatchSummary: summary,
				PostedAt:     &postDate,
			}

			if err := s.store.SaveJob(ctx, job); err != nil {
				log.Printf("Ingestion: failed to save job %s: %v", raw.URL, err)
			}
		}

		if err := s.store.MarkSourceScraped(ctx, src.ID); err != nil {
			log.Printf("Ingestion: failed to mark source %d scraped: %v", src.ID, err)
		}
	}
}

func (s *IngestionService) cleanupLoop(ctx context.Context, interval, retention time.Duration) {
	s.cleanup(ctx, retention)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanup(ctx, retention)
		}
	}
}

func (s *IngestionService) cleanup(ctx context.Context, retention time.Duration) {
	deleted, err := s.store.DeleteOldJobs(ctx, retention)
	if err != nil {
		log.Printf("Ingestion: cleanup failed: %v", err)
		return
	}
	if deleted > 0 {
		log.Printf("Ingestion: cleanup removed %d expired jobs", deleted)
	}
}

func (s *IngestionService) scoreJob(ctx context.Context, title, description string) (int, string) {
	ruleScore := s.ruleScore(title + " " + description)
	if ruleScore == 0 {
		return 0, ""
	}

	match, err := s.matcher.Match(ctx, title, description, s.profile)
	if err != nil {
		log.Printf("Ingestion: AI match failed, using rule score only: %v", err)
		return ruleScore, "Rule-based match only"
	}

	final := int(float64(ruleScore)*0.4 + float64(match.MatchScore)*0.6)
	return final, match.ShortSummary
}

func (s *IngestionService) ruleScore(text string) int {
	lower := strings.ToLower(text)
	hits := 0
	for _, kw := range s.keywords {
		if strings.Contains(lower, kw) {
			hits++
		}
	}

	switch {
	case hits >= 4:
		return 95
	case hits == 3:
		return 85
	case hits == 2:
		return 75
	case hits == 1:
		return 60
	default:
		return 0
	}
}
