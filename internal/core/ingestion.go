package core

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/baxromumarov/job-hunter/internal/ai"
	"github.com/baxromumarov/job-hunter/internal/content"
	"github.com/baxromumarov/job-hunter/internal/httpx"
	"github.com/baxromumarov/job-hunter/internal/observability"
	"github.com/baxromumarov/job-hunter/internal/scraper"
	"github.com/baxromumarov/job-hunter/internal/store"
	"github.com/baxromumarov/job-hunter/internal/urlutil"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

type IngestionService struct {
	store      *store.Store
	matcher    *MatcherService
	normalizer scraper.Normalizer
	keywords   []string
	blockedLoc []string
	blockedJob []string
	profile    ai.CandidateProfile
	fetcher    *httpx.CollyFetcher
	minMatch   int
	hostLimits map[string]*rate.Limiter
	hostMu     sync.Mutex
}

func NewIngestionService(store *store.Store, matcher *MatcherService) *IngestionService {
	minMatch := clampMatchScore(intFromEnv("JOB_MIN_MATCH_SCORE", 60))
	return &IngestionService{
		store:      store,
		matcher:    matcher,
		normalizer: scraper.NewSimpleNormalizer(),
		keywords: []string{
			"golang",
			"go developer",
			"go engineer",
			"backend",
			"backend engineer",
			"backend developer",
			"platform engineer",
			"infrastructure engineer",
			"distributed systems",
			"microservices",
			"grpc",
			"api",
			"software engineer",
			"site reliability",
			"sre",
		},
		blockedLoc: []string{"india", "delhi", "mumbai", "bangalore", "bengaluru", "korea", "south korea", "seoul", "japan", "tokyo", "china", "beijing", "shanghai"},
		blockedJob: []string{
			"sales",
			"account executive",
			"account manager",
			"account director",
			"business development",
			"bdm",
			"bdr",
			"sdr",
			"customer success",
			"marketing",
			"recruiter",
			"talent acquisition",
			"human resources",
			"hr ",
			"finance",
			"accounting",
			"legal",
			"partnership",
			"partnerships",
			"salesforce",
		},
		profile: ai.CandidateProfile{
			TechStack: []string{"golang", "backend", "grpc", "rest", "postgresql", "redis", "docker", "linux"},
		},
		fetcher:    httpx.NewCollyFetcher("job-hunter-bot/1.0"),
		minMatch:   minMatch,
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
		observability.IncError(observability.ErrorStore, "ingestion")
		slog.Error("ingestion list sources failed", "error", err)
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
		observability.IncError(observability.ErrorStore, "ingestion")
		slog.Error("ingestion cleanup failed", "error", err)
		return
	}
	if deleted > 0 {
		slog.Info("ingestion cleanup removed expired jobs", "count", deleted)
	}
}

func (s *IngestionService) scoreJob(ctx context.Context, title, description string) (int, string) {
	if s.isBlockedJob(title, description) {
		return 0, ""
	}
	ruleScore := s.ruleScore(title + " " + description)
	if ruleScore == 0 {
		return 0, ""
	}

	match, err := s.matcher.Match(ctx, title, description, s.profile)
	if err != nil {
		observability.IncError(observability.ErrorAI, "ingestion")
		slog.Warn("ingestion ai match failed", "error", err)
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

func (s *IngestionService) isBlockedJob(title, description string) bool {
	text := strings.ToLower(strings.TrimSpace(title + " " + description))
	if text == "" {
		return false
	}
	for _, kw := range s.blockedJob {
		if kw == "" {
			continue
		}
		if strings.Contains(text, kw) {
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
	case strings.Contains(host, "ashbyhq.com"):
		return scraper.NewAshbyScraper(rawURL)
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
	start := time.Now()
	defer observability.ObserveCrawlDuration(src.Type, time.Since(start).Seconds())

	limiter := s.hostLimiter(src.URL)
	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			observability.IncError(observability.ErrorRateLimit, "ingestion")
			return
		}
	}

	scr := s.pickScraper(src.URL, src.Type)
	rawJobs, err := scr.FetchJobs(since)
	if err != nil {
		errType := observability.ClassifyScrapeError(err)
		observability.IncError(errType, "ingestion")
		_ = s.store.MarkSourceError(ctx, src.ID, errType, err.Error())
		slog.Error("ingestion scrape failed", "url", src.URL, "error", err)
		return
	}
	if len(rawJobs) == 0 {
		if retried, handled := s.retrySource(ctx, src, scr, since); handled {
			rawJobs = retried
		}
	}
	if len(rawJobs) == 0 {
		observability.IncSourcesZeroJobs(src.Type)
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
		if finalScore < s.minMatch {
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
			observability.IncError(observability.ErrorStore, "ingestion")
			_ = s.store.MarkSourceError(ctx, src.ID, observability.ErrorStore, err.Error())
			slog.Error("ingestion save job failed", "url", raw.URL, "error", err)
			continue
		}
		observability.IncJobsDiscovered(src.Type)
		observability.IncJobsExtracted(src.Type)
	}

	if err := s.store.MarkSourceScraped(ctx, src.ID); err != nil {
		observability.IncError(observability.ErrorStore, "ingestion")
		slog.Error("ingestion mark source scraped failed", "source_id", src.ID, "error", err)
		return
	}
	_ = s.store.ClearSourceError(ctx, src.ID)
}

func (s *IngestionService) retrySource(ctx context.Context, src store.Source, scr scraper.JobScraper, since time.Time) ([]scraper.RawJob, bool) {
	if src.Type == "job_board" {
		return nil, false
	}

	signals, err := content.Analyze(ctx, s.fetcher, src.URL)
	if err != nil {
		errType := observability.ClassifyFetchError(err)
		observability.IncError(errType, "ingestion")
		_ = s.store.MarkSourceError(ctx, src.ID, errType, err.Error())
		slog.Error("ingestion retry analyze failed", "url", src.URL, "error", err)
		return nil, false
	}

	if len(signals.ATSLinks) > 0 {
		observability.IncATSDetected("ingestion")
		s.addATSSources(ctx, signals.ATSLinks)
		if normalized, host, err := urlutil.Normalize(src.URL); err == nil && host != "" && !urlutil.IsATSHost(host) {
			if err := s.store.MarkHostATSBacked(ctx, host); err != nil {
				observability.IncError(observability.ErrorStore, "ingestion")
				slog.Error("ingestion ATS-backed mark failed", "url", normalized, "error", err)
			}
		}
		s.markSourceNonJobPermanent(ctx, src, "ats_link", true)
		return nil, true
	}

	if !content.HasJobSignals(signals) {
		return nil, false
	}

	if src.RecheckCount >= 1 {
		s.markSourceNonJobPermanent(ctx, src, "no_jobs_after_retry", false)
		return nil, true
	}

	retryable, ok := scr.(scraper.RelaxedScraper)
	if !ok {
		s.markSourceNonJobPermanent(ctx, src, "no_jobs_after_retry", false)
		return nil, true
	}

	if err := s.store.IncrementSourceRecheck(ctx, src.ID); err != nil {
		observability.IncError(observability.ErrorStore, "ingestion")
		slog.Error("ingestion recheck increment failed", "source_id", src.ID, "error", err)
		return nil, true
	}

	jobs, err := retryable.FetchJobsRelaxed(since)
	if err != nil {
		errType := observability.ClassifyScrapeError(err)
		observability.IncError(errType, "ingestion")
		_ = s.store.MarkSourceError(ctx, src.ID, errType, err.Error())
		slog.Error("ingestion retry scrape failed", "url", src.URL, "error", err)
		return nil, true
	}
	if len(jobs) == 0 {
		s.markSourceNonJobPermanent(ctx, src, "no_jobs_after_retry", false)
	}
	return jobs, true
}

func (s *IngestionService) addATSSources(ctx context.Context, links []string) {
	seen := make(map[string]struct{})
	for _, link := range links {
		normalized, host, err := urlutil.NormalizeATSLink(link)
		if err != nil || host == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}

		pageType := urlutil.PageTypeJobList
		canonicalURL, isAlias, err := s.store.ResolveCanonicalSource(ctx, normalized, host, pageType)
		if err != nil {
			observability.IncError(observability.ErrorStore, "ingestion")
			slog.Error("ingestion canonical resolve failed", "url", normalized, "error", err)
			continue
		}
		if isAlias {
			_, _, _ = s.store.AddSource(ctx, normalized, "job_board", pageType, true, canonicalURL, false, false, 0, "alias", false)
			continue
		}

		observability.IncSourcesPromoted("ingestion")
		if _, _, err := s.store.AddSource(ctx, normalized, "job_board", pageType, false, "", true, true, 0.9, "ats_link", false); err != nil {
			observability.IncError(observability.ErrorStore, "ingestion")
			slog.Error("ingestion store ATS source failed", "url", normalized, "error", err)
		}
	}
}

func (s *IngestionService) markSourceNonJobPermanent(ctx context.Context, src store.Source, reason string, atsBacked bool) {
	confidence := 0.2
	if atsBacked {
		confidence = 0.9
	}
	if _, _, err := s.store.AddSource(ctx, src.URL, src.Type, urlutil.PageTypeNonJobPermanent, false, "", false, false, confidence, reason, atsBacked); err != nil {
		observability.IncError(observability.ErrorStore, "ingestion")
		slog.Error("ingestion mark non-job failed", "url", src.URL, "error", err)
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

func intFromEnv(name string, fallback int) int {
	val := strings.TrimSpace(os.Getenv(name))
	if val == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func clampMatchScore(score int) int {
	switch {
	case score < 0:
		return 0
	case score > 100:
		return 100
	default:
		return score
	}
}
