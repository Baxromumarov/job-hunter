package content

import (
	"context"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"github.com/baxromumarov/job-hunter/internal/httpx"
	"github.com/baxromumarov/job-hunter/internal/urlutil"
	"github.com/gocolly/colly/v2"
)

const maxTextSample = 5000

// jobTitlePattern detects job titles in page title, h1, or URL path
var jobTitlePattern = regexp.MustCompile(`(?i)(engineer|developer|backend|frontend|full.?stack|devops|platform)`)
var salaryPattern = regexp.MustCompile(`(?i)(\$\s?\d{2,3}(?:[.,]\d{3})?(?:k)?|\b(usd|eur|gbp|salary|compensation)\b)`)
var locationPattern = regexp.MustCompile(`(?i)\b(location|remote|hybrid|onsite)\b`)

var jobKeywordPhrases = []string{
	"open positions",
	"open position",
	"job openings",
	"job opening",
	"current openings",
	"current positions",
	"open roles",
	"open role",
	"career opportunities",
	"join our team",
	"join the team",
	"work with us",
	"we're hiring",
	"we are hiring",
	"vacancies",
	"positions",
	"careers",
}

var applyPhrases = []string{
	"apply",
	"apply now",
	"apply today",
}

var jobLinkKeywords = []string{
	"job",
	"jobs",
	"career",
	"careers",
	"opening",
	"openings",
	"position",
	"positions",
	"role",
	"roles",
	"vacancy",
	"vacancies",
}

type Signals struct {
	Title        string
	Meta         string
	Text         string
	ATSLinks     []string
	JobPosting   bool
	JobLinkCount int
	KeywordHits  int
	ApplyHits    int
	// Fix #1: Track if the page itself is on an ATS host
	IsATSPage bool
	// Fix #2: Track title pattern matches
	TitleMatch    bool
	H1Match       bool
	URLMatch      bool
	SalaryMatch   bool
	LocationMatch bool
}

type Decision struct {
	PageType   string
	Reason     string
	Confidence float64
}

func Analyze(ctx context.Context, fetcher *httpx.CollyFetcher, rawURL string) (Signals, error) {
	var signals Signals
	if fetcher == nil {
		fetcher = httpx.NewCollyFetcher("job-hunter-bot/1.0")
	}

	base, err := url.Parse(rawURL)
	if err != nil {
		return signals, err
	}

	// Fix #1: Check if this page itself is on an ATS host
	if urlutil.IsATSHost(base.Hostname()) {
		signals.IsATSPage = true
		signals.JobPosting = true // ATS pages are job sources by definition
		return signals, nil
	}

	// Fix #2: Check URL path for job title patterns
	if jobTitlePattern.MatchString(base.Path) {
		signals.URLMatch = true
	}

	jobLinks := make(map[string]struct{})
	atsLinks := make(map[string]struct{})

	err = fetcher.Fetch(ctx, rawURL, func(c *colly.Collector) {
		c.OnHTML("title", func(e *colly.HTMLElement) {
			if signals.Title == "" {
				signals.Title = strings.TrimSpace(e.Text)
				// Fix #2: Check title for job patterns
				if jobTitlePattern.MatchString(signals.Title) {
					signals.TitleMatch = true
				}
			}
		})
		c.OnHTML("meta[name='description']", func(e *colly.HTMLElement) {
			if signals.Meta == "" {
				signals.Meta = strings.TrimSpace(e.Attr("content"))
			}
		})
		// Fix #2: Check h1 for job title patterns
		c.OnHTML("h1", func(e *colly.HTMLElement) {
			h1Text := strings.TrimSpace(e.Text)
			if h1Text != "" && jobTitlePattern.MatchString(h1Text) {
				signals.H1Match = true
			}
		})
		c.OnHTML("script[type='application/ld+json']", func(e *colly.HTMLElement) {
			if signals.JobPosting {
				return
			}
			if hasJobPostingJSONLD(e.Text) {
				signals.JobPosting = true
			}
		})
		c.OnHTML("body", func(e *colly.HTMLElement) {
			if signals.Text != "" {
				return
			}
			signals.Text = limitText(e.Text)
		})
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			href := strings.TrimSpace(e.Attr("href"))
			if href == "" {
				return
			}
			resolved := resolveLink(base, href)
			if resolved == "" {
				return
			}
			normalized, host, err := urlutil.Normalize(resolved)
			if err != nil || host == "" {
				return
			}
			if urlutil.IsATSHost(host) {
				atsLinks[normalized] = struct{}{}
				return
			}
			if !sameHost(base, host) {
				return
			}
			if !urlutil.IsCrawlable(normalized) {
				return
			}
			if isJobAnchor(href, e.Text) {
				jobLinks[normalized] = struct{}{}
			}
		})
	})
	if err != nil {
		return signals, err
	}

	signals.ATSLinks = mapKeys(atsLinks)
	signals.JobLinkCount = len(jobLinks)

	combined := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		signals.Title,
		signals.Meta,
		signals.Text,
	}, " ")))
	signals.SalaryMatch = salaryPattern.MatchString(combined)
	signals.LocationMatch = locationPattern.MatchString(combined)
	signals.KeywordHits = countHits(combined, jobKeywordPhrases)
	signals.ApplyHits = countHits(combined, applyPhrases)

	return signals, nil
}

func Classify(signals Signals) Decision {
	// Fix #3: Any ONE strong signal is enough
	// Strong signals: ATS page/link, job title pattern, apply button, salary/location, job posting schema

	// Fix #1: ATS page is always a job source
	if signals.IsATSPage {
		return Decision{PageType: urlutil.PageTypeJobList, Reason: "ats_page", Confidence: 0.95}
	}

	// ATS links found on page
	if len(signals.ATSLinks) > 0 {
		return Decision{PageType: urlutil.PageTypeCareerRoot, Reason: "ats_link", Confidence: 0.9}
	}

	// JSON-LD JobPosting schema
	if signals.JobPosting {
		return Decision{PageType: urlutil.PageTypeJobList, Reason: "jsonld_jobposting", Confidence: 0.9}
	}

	// Fix #2 & #3: Title pattern match is a strong signal
	if signals.TitleMatch {
		return Decision{PageType: urlutil.PageTypeJobList, Reason: "title_pattern", Confidence: 0.85}
	}

	// Fix #2 & #3: H1 pattern match is a strong signal
	if signals.H1Match {
		return Decision{PageType: urlutil.PageTypeJobList, Reason: "h1_pattern", Confidence: 0.8}
	}

	// Fix #2 & #3: URL path pattern match is a strong signal
	if signals.URLMatch {
		return Decision{PageType: urlutil.PageTypeJobList, Reason: "url_pattern", Confidence: 0.75}
	}

	// Fix #3: Apply button is a strong signal (lowered from requiring other signals)
	if signals.ApplyHits > 0 {
		return Decision{PageType: urlutil.PageTypeJobList, Reason: "apply_button", Confidence: 0.7}
	}

	// Fix #3: Salary/location patterns are strong signals
	if signals.SalaryMatch {
		return Decision{PageType: urlutil.PageTypeJobList, Reason: "salary_pattern", Confidence: 0.7}
	}
	if signals.LocationMatch {
		return Decision{PageType: urlutil.PageTypeJobList, Reason: "location_pattern", Confidence: 0.7}
	}

	// Job links - lowered threshold from 3 to 1
	if signals.JobLinkCount >= 1 {
		return Decision{PageType: urlutil.PageTypeJobList, Reason: "job_links", Confidence: 0.7}
	}

	// Keyword hits alone are enough
	if signals.KeywordHits > 0 {
		return Decision{PageType: urlutil.PageTypeCareerRoot, Reason: "job_keywords", Confidence: 0.6}
	}

	return Decision{PageType: urlutil.PageTypeNonJob, Reason: "no_job_signals", Confidence: 0.2}
}

// ClassifyWithLogging wraps Classify with debug logging for rejected pages
func ClassifyWithLogging(url string, signals Signals) Decision {
	decision := Classify(signals)

	// Fix #4: Log why pages are rejected
	if decision.PageType == urlutil.PageTypeNonJob {
		slog.Info("Rejected page",
			"url", url,
			"jobPosting", signals.JobPosting,
			"isATSPage", signals.IsATSPage,
			"keywordHits", signals.KeywordHits,
			"jobLinks", signals.JobLinkCount,
			"applyHits", signals.ApplyHits,
			"atsLinks", len(signals.ATSLinks),
			"titleMatch", signals.TitleMatch,
			"h1Match", signals.H1Match,
			"urlMatch", signals.URLMatch,
			"salaryMatch", signals.SalaryMatch,
			"locationMatch", signals.LocationMatch,
			"reason", decision.Reason,
		)
	} else {
		slog.Debug("Accepted page",
			"url", url,
			"pageType", decision.PageType,
			"reason", decision.Reason,
			"confidence", decision.Confidence,
		)
	}

	return decision
}

func HasJobSignals(signals Signals) bool {
	// Fix #3: Include new signals in check
	if signals.IsATSPage {
		return true
	}
	if len(signals.ATSLinks) > 0 {
		return true
	}
	if signals.JobPosting {
		return true
	}
	if signals.TitleMatch || signals.H1Match || signals.URLMatch {
		return true
	}
	if signals.SalaryMatch || signals.LocationMatch {
		return true
	}
	if signals.JobLinkCount > 0 {
		return true
	}
	if signals.KeywordHits > 0 || signals.ApplyHits > 0 {
		return true
	}
	return false
}

func mapKeys(input map[string]struct{}) []string {
	if len(input) == 0 {
		return nil
	}
	out := make([]string, 0, len(input))
	for key := range input {
		out = append(out, key)
	}
	return out
}

func countHits(text string, phrases []string) int {
	if text == "" {
		return 0
	}
	hits := 0
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			hits++
		}
	}
	return hits
}

func isJobAnchor(href, text string) bool {
	lower := strings.ToLower(strings.TrimSpace(href + " " + text))
	for _, kw := range jobLinkKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func limitText(text string) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}
	if len(clean) > maxTextSample {
		return clean[:maxTextSample]
	}
	return clean
}

func resolveLink(base *url.URL, href string) string {
	if strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	return u.String()
}

func sameHost(base *url.URL, host string) bool {
	if base == nil || host == "" {
		return false
	}
	baseHost := strings.ToLower(strings.TrimPrefix(base.Hostname(), "www."))
	targetHost := strings.ToLower(strings.TrimPrefix(host, "www."))
	return baseHost == targetHost
}
