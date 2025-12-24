package content

import (
	"context"
	"net/url"
	"strings"

	"github.com/baxromumarov/job-hunter/internal/httpx"
	"github.com/baxromumarov/job-hunter/internal/urlutil"
	"github.com/gocolly/colly/v2"
)

const maxTextSample = 5000

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

	jobLinks := make(map[string]struct{})
	atsLinks := make(map[string]struct{})

	err = fetcher.Fetch(ctx, rawURL, func(c *colly.Collector) {
		c.OnHTML("title", func(e *colly.HTMLElement) {
			if signals.Title == "" {
				signals.Title = strings.TrimSpace(e.Text)
			}
		})
		c.OnHTML("meta[name='description']", func(e *colly.HTMLElement) {
			if signals.Meta == "" {
				signals.Meta = strings.TrimSpace(e.Attr("content"))
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
	signals.KeywordHits = countHits(combined, jobKeywordPhrases)
	signals.ApplyHits = countHits(combined, applyPhrases)

	return signals, nil
}

func Classify(signals Signals) Decision {
	switch {
	case len(signals.ATSLinks) > 0:
		return Decision{PageType: urlutil.PageTypeCareerRoot, Reason: "ats_link", Confidence: 0.9}
	case signals.JobPosting:
		return Decision{PageType: urlutil.PageTypeJobList, Reason: "jsonld_jobposting", Confidence: 0.9}
	case signals.JobLinkCount >= 3:
		return Decision{PageType: urlutil.PageTypeJobList, Reason: "job_links", Confidence: 0.8}
	case signals.JobLinkCount > 0:
		return Decision{PageType: urlutil.PageTypeCareerRoot, Reason: "job_links", Confidence: 0.7}
	case signals.KeywordHits > 0 || signals.ApplyHits > 0:
		return Decision{PageType: urlutil.PageTypeCareerRoot, Reason: "job_keywords", Confidence: 0.6}
	default:
		return Decision{PageType: urlutil.PageTypeNonJob, Reason: "no_job_signals", Confidence: 0.2}
	}
}

func HasJobSignals(signals Signals) bool {
	if len(signals.ATSLinks) > 0 {
		return true
	}
	if signals.JobPosting {
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
