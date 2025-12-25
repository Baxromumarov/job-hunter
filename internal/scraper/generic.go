package scraper

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/baxromumarov/job-hunter/internal/httpx"
	"github.com/baxromumarov/job-hunter/internal/observability"
	"github.com/baxromumarov/job-hunter/internal/urlutil"
	"github.com/gocolly/colly/v2"
	"golang.org/x/net/html"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type GenericScraper struct {
	BaseURL string
	fetcher *httpx.CollyFetcher
}

func NewGenericScraper(baseURL string) *GenericScraper {
	return &GenericScraper{
		BaseURL: baseURL,
		fetcher: httpx.NewCollyFetcher("job-hunter-bot/1.0"),
	}
}

func (s *GenericScraper) FetchJobs(since time.Time) ([]RawJob, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	base, _ := url.Parse(s.BaseURL)

	if jobs := s.findJSONLDJobs(ctx, base); len(jobs) > 0 {
		return filterJobsSince(jobs, since), nil
	}

	candidates := s.collectDetailLinks(ctx, s.BaseURL, false)
	for _, probe := range probePaths(base) {
		if len(candidates) >= 50 {
			break
		}
		candidates = append(candidates, s.collectDetailLinks(ctx, probe, false)...)
	}

	seen := make(map[string]struct{})
	var jobs []RawJob
	for _, link := range candidates {
		if len(jobs) >= 40 {
			break
		}
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}

		if job := s.extractJob(ctx, link, base); job != nil {
			if !job.PostedAt.IsZero() && job.PostedAt.Before(since) {
				continue
			}
			jobs = append(jobs, *job)
		}
	}

	return jobs, nil
}

func (s *GenericScraper) FetchJobsRelaxed(since time.Time) ([]RawJob, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	base, _ := url.Parse(s.BaseURL)

	if jobs := s.findJSONLDJobs(ctx, base); len(jobs) > 0 {
		return filterJobsSince(jobs, since), nil
	}

	candidates := s.collectDetailLinks(ctx, s.BaseURL, true)
	for _, probe := range probePaths(base) {
		if len(candidates) >= 80 {
			break
		}
		candidates = append(candidates, s.collectDetailLinks(ctx, probe, true)...)
	}
	if len(candidates) == 0 {
		candidates = append(candidates, s.BaseURL)
	}

	seen := make(map[string]struct{})
	var jobs []RawJob
	for _, link := range candidates {
		if len(jobs) >= 60 {
			break
		}
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}

		if job := s.extractJob(ctx, link, base); job != nil {
			if !job.PostedAt.IsZero() && job.PostedAt.Before(since) {
				continue
			}
			jobs = append(jobs, *job)
		}
	}

	return jobs, nil
}

// ExtractText is a helper to get text from HTML
func ExtractText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(ExtractText(c))
	}
	return sb.String()
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

func pathTitleFromURL(u string) string {
	parts := strings.Split(u, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if p == "" {
			continue
		}
		p = strings.ReplaceAll(p, "-", " ")
		p = strings.ReplaceAll(p, "_", " ")
		return cases.Title(language.Und).String(p)
	}
	return ""
}

func hostCompany(base *url.URL) string {
	if base == nil {
		return "Unknown"
	}
	host := strings.TrimPrefix(base.Host, "www.")
	return host
}

func sameHost(base *url.URL, host string) bool {
	if base == nil || host == "" {
		return false
	}
	baseHost := strings.ToLower(strings.TrimPrefix(base.Hostname(), "www."))
	targetHost := strings.ToLower(strings.TrimPrefix(host, "www."))
	return baseHost == targetHost
}

func probePaths(base *url.URL) []string {
	if base == nil {
		return nil
	}
	candidates := []string{
		"/careers",
		"/jobs",
		"/careers/jobs",
		"/join-us",
		"/work-with-us",
		"/opportunities",
		"/teams",
		"/engineering",
		"/early-careers",
		"/company/careers",
	}
	var out []string
	for _, p := range candidates {
		next := *base
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		next.Path = p
		out = append(out, next.String())
	}
	return out
}

func (s *GenericScraper) collectDetailLinks(ctx context.Context, pageURL string, relaxed bool) []string {
	pageBase, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}

	baseIsATS := urlutil.IsATSHost(pageBase.Hostname())
	seen := make(map[string]struct{})
	var links []string

	if err := s.fetcher.Fetch(ctx, pageURL, func(c *colly.Collector) {
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			href := e.Attr("href")
			if href == "" {
				return
			}

			if !relaxed && !baseIsATS {
				lower := strings.ToLower(href + " " + strings.TrimSpace(e.Text))
				if !strings.Contains(lower, "job") &&
					!strings.Contains(lower, "career") &&
					!strings.Contains(lower, "opening") &&
					!strings.Contains(lower, "position") {
					return
				}
			}

			resolved := resolveLink(pageBase, href)
			if resolved == "" {
				return
			}

			normalized, host, err := urlutil.Normalize(resolved)
			if err != nil || host == "" {
				return
			}

			if urlutil.IsATSHost(host) && !baseIsATS {
				return
			}

			if !sameHost(pageBase, host) {
				return
			}

			if !urlutil.IsCrawlable(normalized) {
				return
			}

			if _, ok := seen[normalized]; ok {
				return
			}
			seen[normalized] = struct{}{}
			links = append(links, normalized)
		})
	}); err != nil {
		observability.IncError(observability.ClassifyFetchError(err), "scraper_generic")
		return links
	}
	observability.IncPagesCrawled("scraper_generic")
	return links
}

func (s *GenericScraper) findJSONLDJobs(ctx context.Context, base *url.URL) []RawJob {
	if base == nil {
		return nil
	}
	pages := append([]string{s.BaseURL}, probePaths(base)...)
	for _, page := range pages {
		jobs := s.extractJSONLDJobsFromPage(ctx, page, base)
		if len(jobs) > 0 {
			return jobs
		}
	}
	return nil
}

func (s *GenericScraper) extractJSONLDJobsFromPage(ctx context.Context, pageURL string, base *url.URL) []RawJob {
	if pageURL == "" {
		return nil
	}
	var jobs []RawJob
	if err := s.fetcher.Fetch(ctx, pageURL, func(c *colly.Collector) {
		c.OnHTML("script[type='application/ld+json']", func(e *colly.HTMLElement) {
			parsed := parseJSONLDJobs(e.Text)
			if len(parsed) == 0 {
				return
			}
			jobs = append(jobs, parsed...)
		})
	}); err != nil {
		observability.IncError(observability.ClassifyFetchError(err), "scraper_generic")
		return nil
	}
	observability.IncPagesCrawled("scraper_generic")
	return normalizeJSONLDJobs(jobs, pageURL, base)
}

func normalizeJSONLDJobs(jobs []RawJob, pageURL string, base *url.URL) []RawJob {
	if len(jobs) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var out []RawJob
	index := 0
	for _, job := range jobs {
		index++
		if job.URL == "" {
			job.URL = pageURL + "#job-" + strconv.Itoa(index)
		}
		if job.Title == "" {
			job.Title = pathTitleFromURL(job.URL)
		}
		if job.Company == "" {
			job.Company = hostCompany(base)
		}
		if job.Title == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(job.URL))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(job.Title))
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, job)
	}
	return out
}

func (s *GenericScraper) extractJob(ctx context.Context, link string, base *url.URL) *RawJob {
	var (
		jsonJob *RawJob
		title   string
		desc    string
	)

	if err := s.fetcher.Fetch(ctx, link, func(c *colly.Collector) {
		c.OnHTML("script[type='application/ld+json']", func(e *colly.HTMLElement) {
			if jsonJob != nil {
				return
			}
			if jobs := parseJSONLDJobs(e.Text); len(jobs) > 0 {
				job := jobs[0]
				jsonJob = &job
			}
		})
		c.OnHTML("h1", func(e *colly.HTMLElement) {
			if title == "" {
				title = strings.TrimSpace(e.Text)
			}
		})
		c.OnHTML("title", func(e *colly.HTMLElement) {
			if title == "" {
				title = strings.TrimSpace(e.Text)
			}
		})
		c.OnHTML("meta[name='description']", func(e *colly.HTMLElement) {
			if desc == "" {
				desc = strings.TrimSpace(e.Attr("content"))
			}
		})
		c.OnHTML("p", func(e *colly.HTMLElement) {
			if desc == "" {
				desc = strings.TrimSpace(e.Text)
			}
		})
	}); err != nil {
		observability.IncError(observability.ClassifyFetchError(err), "scraper_generic")
		return nil
	}
	observability.IncPagesCrawled("scraper_generic")

	if jsonJob != nil {
		if jsonJob.URL == "" {
			jsonJob.URL = link
		}
		if jsonJob.Title == "" {
			jsonJob.Title = title
		}
		if jsonJob.Description == "" {
			jsonJob.Description = desc
		}
		if jsonJob.Company == "" {
			jsonJob.Company = hostCompany(base)
		}
		if jsonJob.Title == "" {
			jsonJob.Title = pathTitleFromURL(link)
		}
		if jsonJob.Title == "" {
			return nil
		}
		return jsonJob
	}

	if title == "" {
		title = pathTitleFromURL(link)
	}
	if title == "" {
		return nil
	}

	return &RawJob{
		URL:         link,
		Title:       title,
		Description: desc,
		Company:     hostCompany(base),
		Location:    "",
	}
}

func filterJobsSince(jobs []RawJob, since time.Time) []RawJob {
	if len(jobs) == 0 {
		return nil
	}
	if since.IsZero() {
		return jobs
	}
	var out []RawJob
	for _, job := range jobs {
		if !job.PostedAt.IsZero() && job.PostedAt.Before(since) {
			continue
		}
		out = append(out, job)
	}
	return out
}

func parseJSONLDJobs(raw string) []RawJob {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	var jobs []RawJob
	findJobPostings(payload, &jobs)
	return jobs
}

func findJobPostings(payload any, out *[]RawJob) {
	switch t := payload.(type) {
	case map[string]any:
		if job := jobFromMap(t); job != nil {
			*out = append(*out, *job)
		}
		if graph, ok := t["@graph"].([]any); ok {
			for _, item := range graph {
				findJobPostings(item, out)
			}
		}
	case []any:
		for _, item := range t {
			findJobPostings(item, out)
		}
	}
}

func jobFromMap(payload map[string]any) *RawJob {
	if !isJobPostingType(payload["@type"]) {
		return nil
	}

	job := &RawJob{
		URL:         stringField(payload["url"]),
		Title:       stringField(payload["title"]),
		Description: stringField(payload["description"]),
		Company:     orgName(payload["hiringOrganization"]),
		Location:    parseLocation(payload["jobLocation"]),
		PostedAt:    parseDate(payload["datePosted"]),
	}

	if job.Title == "" && job.Description == "" {
		return nil
	}
	return job
}

func stringField(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case map[string]any:
		if val, ok := t["@value"]; ok {
			if str, ok2 := val.(string); ok2 {
				return strings.TrimSpace(str)
			}
		}
	}
	return ""
}

func isJobPostingType(t any) bool {
	switch v := t.(type) {
	case string:
		return v == "JobPosting"
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == "JobPosting" {
				return true
			}
		}
	}
	return false
}

func orgName(v any) string {
	if name := stringField(v); name != "" {
		return name
	}
	if org, ok := v.(map[string]any); ok {
		return stringField(org["name"])
	}
	return ""
}

func parseLocation(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []any:
		for _, item := range t {
			if loc := parseLocation(item); loc != "" {
				return loc
			}
		}
	case map[string]any:
		if addr, ok := t["address"].(map[string]any); ok {
			return joinParts(
				stringField(addr["addressLocality"]),
				stringField(addr["addressRegion"]),
				stringField(addr["addressCountry"]),
			)
		}
		if name := stringField(t["name"]); name != "" {
			return name
		}
	}
	return ""
}

func parseDate(v any) time.Time {
	val := stringField(v)
	if val == "" {
		return time.Time{}
	}
	layouts := []string{time.RFC3339, time.RFC3339Nano, "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, val); err == nil {
			return t
		}
	}
	return time.Time{}
}

func joinParts(parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(p))
	}
	return strings.Join(out, ", ")
}
