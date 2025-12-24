package core

import (
	"context"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/baxromumarov/job-hunter/internal/ai"
	"github.com/baxromumarov/job-hunter/internal/scraper"
	"github.com/baxromumarov/job-hunter/internal/store"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

type IngestionService struct {
	store      *store.Store
	matcher    *MatcherService
	normalizer scraper.Normalizer
	keywords   []string
	blockedLoc []string
	profile    ai.CandidateProfile
	hostLimits map[string]*rate.Limiter
	hostMu     sync.Mutex
}

func NewIngestionService(store *store.Store, matcher *MatcherService) *IngestionService {
	return &IngestionService{
		store:      store,
		matcher:    matcher,
		normalizer: scraper.NewSimpleNormalizer(),
		keywords:   []string{"golang", "go developer", "backend", "microservices", "grpc", "distributed systems", "software engineer"},
		blockedLoc: []string{"india", "delhi", "mumbai", "bangalore", "bengaluru", "korea", "south korea", "seoul", "japan", "tokyo", "china", "beijing", "shanghai"},
		profile: ai.CandidateProfile{
			TechStack: []string{"golang", "backend", "grpc", "rest", "postgresql", "redis", "docker", "linux"},
		},
		hostLimits: make(map[string]*rate.Limiter),
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
	sources, _, err := s.store.ListSources(ctx, 200, 0)
	if err != nil {
		log.Printf("Ingestion: failed to list sources: %v", err)
		return
	}

	since := time.Now().Add(-10 * 24 * time.Hour)

	workerCount := 6
	srcCh := make(chan store.Source)

	g, gctx := errgroup.WithContext(ctx)
	for i := 0; i < workerCount; i++ {
		g.Go(func() error {
			for src := range srcCh {
				select {
				case <-gctx.Done():
					return gctx.Err()
				default:
				}
				s.processSource(gctx, src, since)
			}
			return nil
		})
	}

	for _, src := range sources {
		if gctx.Err() != nil {
			break
		}
		srcCh <- src
	}
	close(srcCh)
	_ = g.Wait()
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

func (s *IngestionService) isBlockedLocation(loc string) bool {
	if loc == "" {
		return false
	}
	l := strings.ToLower(loc)
	for _, b := range s.blockedLoc {
		if strings.Contains(l, b) {
			return true
		}
	}
	return false
}

func (s *IngestionService) pickScraper(rawURL, sourceType string) scraper.JobScraper {
	u, err := url.Parse(rawURL)
	if err != nil {
		return scraper.NewGenericScraper(rawURL)
	}
	host := u.Host

	switch {
	case strings.Contains(host, "remoteok.com"):
		return scraper.NewRemoteOKScraper("golang")
	case strings.Contains(host, "weworkremotely.com"):
		return scraper.NewWWRScraper()
	case strings.Contains(host, "lever.co"):
		return scraper.NewLeverScraper(rawURL)
	case strings.Contains(host, "greenhouse.io") || strings.Contains(host, "boards.greenhouse.io"):
		return scraper.NewGreenhouseScraper(rawURL)
	default:
		// Fall back to generic
		_ = sourceType
		return scraper.NewGenericScraper(rawURL)
	}
}

func (s *IngestionService) processSource(ctx context.Context, src store.Source, since time.Time) {
	limiter := s.hostLimiter(src.URL)
	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return
		}
	}

	scr := s.pickScraper(src.URL, src.Type)
	rawJobs, err := scr.FetchJobs(since)
	if err != nil {
		log.Printf("Ingestion: scrape failed for %s: %v", src.URL, err)
		return
	}

	for _, raw := range rawJobs {
		select {
		case <-ctx.Done():
			return
		default:
		}

		postDate := raw.PostedAt
		if !postDate.IsZero() && postDate.Before(since) {
			continue
		}

		desc := raw.Description
		if normalized, err := s.normalizer.Normalize(raw.Description); err == nil && normalized != "" {
			desc = normalized
		}

		if s.isBlockedLocation(raw.Location) {
			continue
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
			PostedAt:     nullableTime(postDate),
		}

		if err := s.store.SaveJob(ctx, job); err != nil {
			log.Printf("Ingestion: failed to save job %s: %v", raw.URL, err)
		}
	}

	if err := s.store.MarkSourceScraped(ctx, src.ID); err != nil {
		log.Printf("Ingestion: failed to mark source %d scraped: %v", src.ID, err)
	}
}

func (s *IngestionService) hostLimiter(rawURL string) *rate.Limiter {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	host := u.Hostname()
	if host == "" {
		return nil
	}
	s.hostMu.Lock()
	defer s.hostMu.Unlock()
	if lim, ok := s.hostLimits[host]; ok {
		return lim
	}
	lim := rate.NewLimiter(rate.Every(time.Second), 2)
	s.hostLimits[host] = lim
	return lim
}

func nullableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
