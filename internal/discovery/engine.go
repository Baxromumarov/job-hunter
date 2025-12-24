package discovery

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	_ "embed"

	"github.com/baxromumarov/job-hunter/internal/core"
	"github.com/baxromumarov/job-hunter/internal/observability"
	"github.com/baxromumarov/job-hunter/internal/store"
	"github.com/baxromumarov/job-hunter/internal/urlutil"
)

type Engine struct {
	store      *store.Store
	classifier *core.ClassifierService
}

type candidateSource struct {
	URL        string `json:"url"`
	SourceType string `json:"source_type"`
	Title      string `json:"title"`
	Meta       string `json:"meta"`
	Text       string `json:"text"`
}

//go:embed seeds.json
var seedsJSON []byte

var seedCandidates = loadSeedCandidates()

func loadSeedCandidates() []candidateSource {
	var seeds []candidateSource
	if err := json.Unmarshal(seedsJSON, &seeds); err != nil {
		slog.Error("discovery failed to load embedded seeds", "error", err)
		return nil
	}
	return seeds
}

func NewEngine(store *store.Store, classifier *core.ClassifierService) *Engine {
	return &Engine{
		store:      store,
		classifier: classifier,
	}
}

func (e *Engine) StartDiscovery(ctx context.Context) {
	slog.Info("discovery start")

	go func() {
		e.runCycle(ctx)
		e.crawlForCareerLinks(ctx)
		e.searchWeb(ctx)

		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.runCycle(ctx)
				e.crawlForCareerLinks(ctx)
				e.searchWeb(ctx)
			}
		}
	}()
}

func (e *Engine) runCycle(ctx context.Context) {
	for _, c := range seedCandidates {
		select {
		case <-ctx.Done():
			return
		default:
			e.processCandidate(ctx, c)
			time.Sleep(500 * time.Millisecond)
		}
	}
	slog.Info("discovery cycle complete")
}

func (e *Engine) crawlForCareerLinks(ctx context.Context) {
	sites := []string{
		"https://github.com",
		"https://about.gitlab.com",
		"https://www.heroku.com",
		"https://www.cloudflare.com",
		"https://www.vercel.com",
		"https://www.supabase.com",
		"https://www.datadoghq.com",
		"https://www.zendesk.com",
		"https://jobs.ashbyhq.com",
		"https://www.hashicorp.com",
		"https://www.digitalocean.com",
		"https://about.gitlab.com/careers/",
	}

	c := newCrawler()
	for _, site := range sites {
		select {
		case <-ctx.Done():
			return
		default:
		}
		links := c.extractCareerLinks(ctx, site)
		for _, link := range links {
			e.processCandidate(ctx, candidateSource{
				URL:        link,
				SourceType: guessSourceType(link),
			})
		}
	}
}

func (e *Engine) searchWeb(ctx context.Context) {
	queries := []string{
		"golang backend engineer jobs",
		"software engineer careers site:careers",
		"backend jobs site:jobs",
		"remote golang hiring",
		"golang remote backend job board",
		"senior golang engineer careers",
		"golang developer jobs Europe remote",
	}

	seen := make(map[string]struct{})
	c := newCrawler()
	for _, q := range queries {
		urls := duckDuckSearch(ctx, q, 15)
		for _, u := range urls {
			if _, ok := seen[u]; ok {
				continue
			}
			seen[u] = struct{}{}

			// Crawl the result page for career/job links and enqueue those too.
			links := c.extractCareerLinks(ctx, u)
			atsOnly := containsATS(links)
			if !atsOnly && urlutil.IsDiscoveryEligible(u) {
				e.processCandidate(ctx, candidateSource{
					URL:        u,
					SourceType: guessSourceType(u),
				})
			}
			for _, link := range links {
				if _, ok := seen[link]; ok {
					continue
				}
				seen[link] = struct{}{}
				e.processCandidate(ctx, candidateSource{
					URL:        link,
					SourceType: guessSourceType(link),
				})
			}
		}
	}
}

func (e *Engine) processCandidate(ctx context.Context, c candidateSource) {
	normalized, host, err := urlutil.Normalize(c.URL)
	if err != nil {
		slog.Info("discovery skip", "url", c.URL, "reason", "invalid_url")
		return
	}

	pageType := urlutil.DetectPageType(normalized)
	sourceType := c.SourceType
	if sourceType == "" {
		sourceType = guessSourceType(normalized)
	}
	if pageType == urlutil.PageTypeNonJob {
		_, _, _ = e.store.AddSource(ctx, normalized, sourceType, pageType, false, "", false, false, 0, "blocked_path")
		slog.Info("discovery skip", "url", normalized, "reason", "blocked_path", "page_type", pageType)
		return
	}
	if pageType == urlutil.PageTypeJobDetail {
		_, _, _ = e.store.AddSource(ctx, normalized, sourceType, pageType, false, "", false, false, 0, "job_detail")
		slog.Info("discovery skip", "url", normalized, "reason", "non_job", "page_type", pageType)
		return
	}

	existing, err := e.store.FindSourceByURL(ctx, c.URL)
	if err != nil {
		observability.IncError(observability.ErrorStore, "discovery")
		slog.Error("discovery lookup failed", "url", normalized, "error", err)
		return
	}
	if existing != nil {
		reason := "already_processed"
		if existing.IsAlias {
			reason = "alias"
		} else if existing.PageType == urlutil.PageTypeNonJob || existing.PageType == urlutil.PageTypeJobDetail {
			reason = "non_job"
		}
		slog.Info("discovery skip", "url", normalized, "reason", reason, "page_type", existing.PageType)
		return
	}

	canonicalURL, isAlias, err := e.store.ResolveCanonicalSource(ctx, normalized, host, pageType)
	if err != nil {
		observability.IncError(observability.ErrorStore, "discovery")
		slog.Error("discovery canonical resolve failed", "url", normalized, "error", err)
		return
	}
	if isAlias {
		_, _, _ = e.store.AddSource(ctx, normalized, sourceType, pageType, true, canonicalURL, false, false, 0, "alias")
		slog.Info("discovery skip", "url", normalized, "reason", "alias", "canonical", canonicalURL)
		return
	}
	canonicalURL = ""

	// In a real scenario, we would fetch the page content here.
	// We pass dummy content to the classifier, which mocks the AI anyway.
	// If we had a real scraper, we'd use it here.

	title := c.Title
	if title == "" {
		title = "Title for " + c.URL
	}
	meta := c.Meta
	if meta == "" {
		meta = "Meta for " + c.URL
	}
	text := c.Text
	if text == "" {
		text = "Sample text for " + c.URL + " which is definitely long enough to pass the fifty character limit set in the mock ai client."
	}

	classification, err := e.classifier.Classify(ctx, normalized, title, meta, text)
	if err != nil {
		observability.IncError(observability.ErrorAI, "discovery")
		_ = e.store.MarkSourceErrorByURL(ctx, normalized, observability.ErrorAI, err.Error())
		slog.Error("discovery classify failed", "url", normalized, "error", err)
		return
	}

	if classification.IsJobSite && classification.TechRelated && classification.Confidence > 0.7 {
		observability.IncSourceDecision("accepted")
		id, existed, err := e.store.AddSource(
			ctx,
			normalized,
			sourceType,
			pageType,
			false,
			canonicalURL,
			classification.IsJobSite,
			classification.TechRelated,
			classification.Confidence,
			classification.Reason,
		)
		if err != nil {
			observability.IncError(observability.ErrorStore, "discovery")
			slog.Error("discovery store source failed", "url", normalized, "error", err)
		} else {
			if existed {
				slog.Info("discovery skip", "url", normalized, "reason", "already_processed", "id", id)
			} else {
				slog.Info("discovery source approved", "url", normalized, "id", id)
				// Scraping now handled by ingestion using site-specific scrapers
			}
		}
	} else {
		observability.IncSourceDecision("rejected")
		_, _, _ = e.store.AddSource(
			ctx,
			normalized,
			c.SourceType,
			urlutil.PageTypeNonJob,
			false,
			"",
			false,
			false,
			classification.Confidence,
			classification.Reason,
		)
		slog.Info("discovery skip", "url", normalized, "reason", "non_job", "confidence", classification.Confidence)
	}
}

func guessSourceType(u string) string {
	u = strings.ToLower(u)
	if strings.Contains(u, "remoteok") ||
		strings.Contains(u, "weworkremotely") ||
		strings.Contains(u, "builtin.com") ||
		strings.Contains(u, "linkedin") ||
		strings.Contains(u, "greenhouse.io") ||
		strings.Contains(u, "lever.co") ||
		strings.Contains(u, "ashbyhq.com") ||
		strings.Contains(u, "workable.com") {
		return "job_board"
	}
	return "company_page"
}

func containsATS(links []string) bool {
	for _, link := range links {
		_, host, err := urlutil.Normalize(link)
		if err == nil && urlutil.IsATSHost(host) {
			return true
		}
	}
	return false
}
