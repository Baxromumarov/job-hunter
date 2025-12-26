package discovery

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	_ "embed"

	"github.com/baxromumarov/job-hunter/internal/content"
	"github.com/baxromumarov/job-hunter/internal/core"
	"github.com/baxromumarov/job-hunter/internal/httpx"
	"github.com/baxromumarov/job-hunter/internal/observability"
	"github.com/baxromumarov/job-hunter/internal/store"
	"github.com/baxromumarov/job-hunter/internal/urlutil"
)

type Engine struct {
	store      *store.Store
	classifier *core.ClassifierService
	fetcher    *httpx.CollyFetcher
}

type candidateSource struct {
	URL        string `json:"url"`
	SourceType string `json:"source_type"`
	Title      string `json:"title"`
	Meta       string `json:"meta"`
	Text       string `json:"text"`
	ParentURL  string `json:"parent_url,omitempty"`
	Depth      int    `json:"depth,omitempty"`
}

//go:embed seeds.json
var seedsJSON []byte

var seedCandidates = loadSeedCandidates()

const maxCandidateDepth = 2

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
		fetcher:    httpx.NewCollyFetcher("job-hunter-bot/1.0"),
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
		if containsATS(links) {
			if normalized, host, err := urlutil.Normalize(site); err == nil && host != "" && !urlutil.IsATSHost(host) {
				if err := e.store.MarkHostATSBacked(ctx, host); err != nil {
					observability.IncError(observability.ErrorStore, "discovery")
					slog.Error("discovery ATS-backed mark failed", "url", normalized, "error", err)
				}
				_, _, _ = e.store.AddSource(ctx, normalized, guessSourceType(normalized), urlutil.PageTypeNonJobHighConfidence, false, "", false, false, 0.9, "ats_link", true)
			}
		}
		for _, link := range links {
			e.processCandidate(ctx, candidateSource{
				URL:        link,
				SourceType: guessSourceType(link),
				ParentURL:  site,
				Depth:      1,
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
			if atsOnly {
				if normalized, host, err := urlutil.Normalize(u); err == nil && host != "" && !urlutil.IsATSHost(host) {
					if err := e.store.MarkHostATSBacked(ctx, host); err != nil {
						observability.IncError(observability.ErrorStore, "discovery")
						slog.Error("discovery ATS-backed mark failed", "url", normalized, "error", err)
					}
					_, _, _ = e.store.AddSource(ctx, normalized, guessSourceType(normalized), urlutil.PageTypeNonJobHighConfidence, false, "", false, false, 0.9, "ats_link", true)
				}
			}
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
					ParentURL:  u,
					Depth:      1,
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
	if urlutil.IsATSHost(host) {
		if atsURL, atsHost, err := urlutil.NormalizeATSLink(normalized); err == nil && atsHost != "" {
			normalized = atsURL
			host = atsHost
		}
	}
	if !urlutil.IsDiscoveryEligible(normalized) {
		slog.Info("discovery skip", "url", normalized, "reason", "ineligible")
		return
	}
	if !urlutil.IsATSHost(host) {
		atsBacked, err := e.store.IsHostATSBacked(ctx, host)
		if err != nil {
			observability.IncError(observability.ErrorStore, "discovery")
			slog.Error("discovery ATS-backed check failed", "url", normalized, "error", err)
		} else if atsBacked {
			slog.Info("discovery skip", "url", normalized, "reason", "ats_backed")
			return
		}
	}
	observability.IncURLsDiscovered("discovery")

	sourceType := c.SourceType
	if sourceType == "" {
		sourceType = guessSourceType(normalized)
	}
	forcedJobBoard := urlutil.IsKnownJobBoardHost(host)

	existing, err := e.store.FindSourceByURL(ctx, normalized)
	if err != nil {
		observability.IncError(observability.ErrorStore, "discovery")
		slog.Error("discovery lookup failed", "url", normalized, "error", err)
		return
	}
	retryAttempt := false
	if existing != nil && existing.PageType != urlutil.PageTypeCandidate && !forcedJobBoard {
		reason := "already_processed"
		if existing.IsAlias {
			reason = "alias"
		} else if isRetryableNonJob(existing) {
			retryAttempt = true
		} else if isFinalNonJob(existing.PageType) {
			reason = "non_job"
		} else if existing.PageType == urlutil.PageTypeJobDetail {
			reason = "non_job"
		}
		if !retryAttempt {
			slog.Info("discovery skip", "url", normalized, "reason", reason, "page_type", existing.PageType)
			return
		}
	}

	if forcedJobBoard {
		pageType := urlutil.PageTypeJobList
		canonicalURL, isAlias, err := e.store.ResolveCanonicalSource(ctx, normalized, host, pageType)
		if err != nil {
			observability.IncError(observability.ErrorStore, "discovery")
			slog.Error("discovery canonical resolve failed", "url", normalized, "error", err)
			return
		}
		if isAlias {
			_, _, _ = e.store.AddSource(ctx, normalized, sourceType, pageType, true, canonicalURL, false, false, 0, "alias", false)
			slog.Info("discovery skip", "url", normalized, "reason", "alias", "canonical", canonicalURL)
			return
		}

		observability.IncSourceDecision("accepted")
		observability.IncSourcesPromoted("discovery")
		id, existed, err := e.store.AddSource(
			ctx,
			normalized,
			sourceType,
			pageType,
			false,
			"",
			true,
			true,
			0.9,
			"job_board_allowlist",
			false,
		)
		if err != nil {
			observability.IncError(observability.ErrorStore, "discovery")
			slog.Error("discovery store source failed", "url", normalized, "error", err)
			return
		}
		if existed {
			slog.Info("discovery skip", "url", normalized, "reason", "already_processed", "id", id)
			return
		}
		slog.Info("discovery source approved", "url", normalized, "id", id)
		return
	}

	if urlutil.IsATSHost(host) {
		pageType := urlutil.PageTypeJobList
		canonicalURL, isAlias, err := e.store.ResolveCanonicalSource(ctx, normalized, host, pageType)
		if err != nil {
			observability.IncError(observability.ErrorStore, "discovery")
			slog.Error("discovery canonical resolve failed", "url", normalized, "error", err)
			return
		}
		if isAlias {
			_, _, _ = e.store.AddSource(ctx, normalized, sourceType, pageType, true, canonicalURL, false, false, 0, "alias", false)
			slog.Info("discovery skip", "url", normalized, "reason", "alias", "canonical", canonicalURL)
			return
		}

		observability.IncSourceDecision("accepted")
		observability.IncSourcesPromoted("discovery")
		id, existed, err := e.store.AddSource(
			ctx,
			normalized,
			sourceType,
			pageType,
			false,
			"",
			true,
			true,
			0.9,
			"ats_host",
			false,
		)
		if err != nil {
			observability.IncError(observability.ErrorStore, "discovery")
			slog.Error("discovery store source failed", "url", normalized, "error", err)
			return
		}
		if existed {
			slog.Info("discovery skip", "url", normalized, "reason", "already_processed", "id", id)
			return
		}
		slog.Info("discovery source approved", "url", normalized, "id", id)
		return
	}

	_, _, _ = e.store.AddSource(
		ctx,
		normalized,
		sourceType,
		urlutil.PageTypeCandidate,
		false,
		"",
		false,
		false,
		0,
		"candidate",
		false,
	)

	signals, err := content.Analyze(ctx, e.fetcher, normalized)
	if err != nil {
		errType := observability.ClassifyFetchError(err)
		observability.IncError(errType, "discovery")
		_ = e.store.MarkSourceErrorByURL(ctx, normalized, errType, err.Error())
		slog.Error("discovery fetch failed", "url", normalized, "error", err)
		return
	}
	observability.IncPagesCrawled("discovery")

	if len(signals.ATSLinks) > 0 {
		observability.IncATSDetected("discovery")
		e.addATSSources(ctx, signals.ATSLinks)
		if err := e.store.MarkHostATSBacked(ctx, host); err != nil {
			observability.IncError(observability.ErrorStore, "discovery")
			slog.Error("discovery ATS-backed mark failed", "url", normalized, "error", err)
		}
		observability.IncSourceDecision("rejected")
		_, _, _ = e.store.AddSource(
			ctx,
			normalized,
			sourceType,
			urlutil.PageTypeNonJobHighConfidence,
			false,
			"",
			false,
			false,
			0.9,
			"ats_link",
			true,
		)
		slog.Info("discovery skip", "url", normalized, "reason", "ats_link", "ats_count", len(signals.ATSLinks))
		return
	}

	// Fix #4: Use ClassifyWithLogging for debug output on rejected pages
	decision := content.ClassifyWithLogging(normalized, signals)
	if decision.PageType == urlutil.PageTypeNonJob {
		pageType := urlutil.PageTypeNonJobLowConfidence
		reason := decision.Reason
		if retryAttempt {
			pageType = urlutil.PageTypeNonJobHighConfidence
			reason = decision.Reason + "_retry"
			if existing != nil {
				if err := e.store.IncrementSourceRecheck(ctx, existing.ID); err != nil {
					observability.IncError(observability.ErrorStore, "discovery")
					slog.Error("discovery recheck increment failed", "source_id", existing.ID, "error", err)
				}
			}
		}
		observability.IncSourceDecision("rejected")
		_, _, _ = e.store.AddSource(
			ctx,
			normalized,
			sourceType,
			pageType,
			false,
			"",
			false,
			false,
			decision.Confidence,
			reason,
			false,
		)
		slog.Info("discovery skip", "url", normalized, "reason", "non_job", "classification", reason)
		if pageType == urlutil.PageTypeNonJobLowConfidence {
			e.discoverChildCandidates(ctx, candidateSource{
				URL:       normalized,
				Depth:     c.Depth,
				ParentURL: c.ParentURL,
			})
		}
		return
	}

	canonicalURL, isAlias, err := e.store.ResolveCanonicalSource(ctx, normalized, host, decision.PageType)
	if err != nil {
		observability.IncError(observability.ErrorStore, "discovery")
		slog.Error("discovery canonical resolve failed", "url", normalized, "error", err)
		return
	}
	if isAlias {
		_, _, _ = e.store.AddSource(ctx, normalized, sourceType, decision.PageType, true, canonicalURL, false, false, 0, "alias", false)
		slog.Info("discovery skip", "url", normalized, "reason", "alias", "canonical", canonicalURL)
		e.promoteParent(ctx, c.ParentURL, "child_"+decision.Reason, decision.Confidence)
		return
	}

	observability.IncSourceDecision("accepted")
	observability.IncSourcesPromoted("discovery")
	id, existed, err := e.store.AddSource(
		ctx,
		normalized,
		sourceType,
		decision.PageType,
		false,
		"",
		true,
		true,
		decision.Confidence,
		decision.Reason,
		false,
	)
	if err != nil {
		observability.IncError(observability.ErrorStore, "discovery")
		slog.Error("discovery store source failed", "url", normalized, "error", err)
		return
	}
	if existed {
		slog.Info("discovery skip", "url", normalized, "reason", "already_processed", "id", id)
		return
	}
	slog.Info("discovery source approved", "url", normalized, "id", id)
	e.promoteParent(ctx, c.ParentURL, "child_"+decision.Reason, decision.Confidence)
	// Scraping now handled by ingestion using site-specific scrapers.
}

func (e *Engine) addATSSources(ctx context.Context, links []string) {
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
		sourceType := "job_board"
		canonicalURL, isAlias, err := e.store.ResolveCanonicalSource(ctx, normalized, host, pageType)
		if err != nil {
			observability.IncError(observability.ErrorStore, "discovery")
			slog.Error("discovery canonical resolve failed", "url", normalized, "error", err)
			continue
		}
		if isAlias {
			_, _, _ = e.store.AddSource(ctx, normalized, sourceType, pageType, true, canonicalURL, false, false, 0, "alias", false)
			continue
		}

		observability.IncSourceDecision("accepted")
		observability.IncSourcesPromoted("discovery")
		if _, _, err := e.store.AddSource(ctx, normalized, sourceType, pageType, false, "", true, true, 0.9, "ats_link", false); err != nil {
			observability.IncError(observability.ErrorStore, "discovery")
			slog.Error("discovery store ATS source failed", "url", normalized, "error", err)
		}
	}
}

func (e *Engine) discoverChildCandidates(ctx context.Context, parent candidateSource) {
	if parent.URL == "" || parent.Depth >= maxCandidateDepth {
		return
	}
	c := newCrawler()
	links, _, rateLimited := c.collectLinksFromPage(ctx, parent.URL)
	if rateLimited {
		return
	}
	for _, link := range links {
		if link == "" || link == parent.URL {
			continue
		}
		e.processCandidate(ctx, candidateSource{
			URL:        link,
			SourceType: guessSourceType(link),
			ParentURL:  parent.URL,
			Depth:      parent.Depth + 1,
		})
	}
}

func (e *Engine) promoteParent(ctx context.Context, parentURL, reason string, confidence float64) {
	if parentURL == "" {
		return
	}
	normalized, host, err := urlutil.Normalize(parentURL)
	if err != nil || host == "" {
		return
	}
	if urlutil.IsATSHost(host) {
		return
	}
	atsBacked, err := e.store.IsHostATSBacked(ctx, host)
	if err != nil {
		observability.IncError(observability.ErrorStore, "discovery")
		slog.Error("discovery ATS-backed check failed", "url", normalized, "error", err)
		return
	}
	if atsBacked {
		return
	}
	if reason == "" {
		reason = "child_signal"
	}
	if confidence < 0.6 {
		confidence = 0.6
	}

	pageType := urlutil.PageTypeCareerRoot
	canonicalURL, isAlias, err := e.store.ResolveCanonicalSource(ctx, normalized, host, pageType)
	if err != nil {
		observability.IncError(observability.ErrorStore, "discovery")
		slog.Error("discovery canonical resolve failed", "url", normalized, "error", err)
		return
	}
	if isAlias {
		_, _, _ = e.store.AddSource(ctx, normalized, guessSourceType(normalized), pageType, true, canonicalURL, false, false, 0, "alias", false)
		return
	}

	observability.IncSourceDecision("accepted")
	observability.IncSourcesPromoted("discovery")
	if _, _, err := e.store.AddSource(ctx, normalized, guessSourceType(normalized), pageType, false, "", true, true, confidence, reason, false); err != nil {
		observability.IncError(observability.ErrorStore, "discovery")
		slog.Error("discovery store parent promotion failed", "url", normalized, "error", err)
	}
}

func guessSourceType(u string) string {
	u = strings.ToLower(u)
	if strings.Contains(u, "remoteok") ||
		strings.Contains(u, "weworkremotely") ||
		strings.Contains(u, "builtin.com") ||
		strings.Contains(u, "linkedin") ||
		strings.Contains(u, "angel.co") ||
		strings.Contains(u, "flexjobs.com") ||
		strings.Contains(u, "freelancer.com") ||
		strings.Contains(u, "jobspresso.co") ||
		strings.Contains(u, "justremote.co") ||
		strings.Contains(u, "nodesk.co") ||
		strings.Contains(u, "outsourcely.com") ||
		strings.Contains(u, "pangian.com") ||
		strings.Contains(u, "remote.co") ||
		strings.Contains(u, "remote4me.com") ||
		strings.Contains(u, "remoteok.io") ||
		strings.Contains(u, "remotecrew.io") ||
		strings.Contains(u, "remotees.com") ||
		strings.Contains(u, "remotehabits.com") ||
		strings.Contains(u, "remotive.com") ||
		strings.Contains(u, "simplyhired.com") ||
		strings.Contains(u, "skipthechive.com") ||
		strings.Contains(u, "toptal.com") ||
		strings.Contains(u, "upwork.com") ||
		strings.Contains(u, "virtualvocations.com") ||
		strings.Contains(u, "workingnomads.com") ||
		strings.Contains(u, "europeremotely.com") ||
		strings.Contains(u, "greenhouse.io") ||
		strings.Contains(u, "lever.co") ||
		strings.Contains(u, "ashbyhq.com") ||
		strings.Contains(u, "workdayjobs.com") ||
		strings.Contains(u, "myworkdayjobs.com") ||
		strings.Contains(u, "smartrecruiters.com") ||
		strings.Contains(u, "bamboohr.com") ||
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

func isRetryableNonJob(src *store.Source) bool {
	if src == nil {
		return false
	}
	switch src.PageType {
	case urlutil.PageTypeNonJob, urlutil.PageTypeNonJobLowConfidence:
		return src.RecheckCount < 1
	default:
		return false
	}
}

func isFinalNonJob(pageType string) bool {
	switch pageType {
	case urlutil.PageTypeNonJob,
		urlutil.PageTypeNonJobLowConfidence,
		urlutil.PageTypeNonJobHighConfidence,
		urlutil.PageTypeNonJobPermanent:
		return true
	default:
		return false
	}
}
