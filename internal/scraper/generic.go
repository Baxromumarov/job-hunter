package scraper

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/baxromumarov/job-hunter/internal/httpx"
	"github.com/baxromumarov/job-hunter/internal/observability"
	"github.com/baxromumarov/job-hunter/internal/urlutil"
	"github.com/gocolly/colly/v2"
	"golang.org/x/net/html"
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

	candidates := s.collectDetailLinks(ctx, s.BaseURL)
	for _, probe := range probePaths(base) {
		if len(candidates) >= 50 {
			break
		}
		candidates = append(candidates, s.collectDetailLinks(ctx, probe)...)
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
		return strings.Title(p)
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

func probePaths(base *url.URL) []string {
	if base == nil {
		return nil
	}
	candidates := []string{"/careers", "/jobs", "/careers/jobs", "/join-us", "/work-with-us"}
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

func (s *GenericScraper) collectDetailLinks(ctx context.Context, pageURL string) []string {
	pageBase, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var links []string
	if err := s.fetcher.Fetch(ctx, pageURL, func(c *colly.Collector) {
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			href := e.Attr("href")
			if href == "" {
				return
			}
			lower := strings.ToLower(href + " " + strings.TrimSpace(e.Text))
			if !strings.Contains(lower, "job") &&
				!strings.Contains(lower, "career") &&
				!strings.Contains(lower, "opening") &&
				!strings.Contains(lower, "position") {
				return
			}

			resolved := resolveLink(pageBase, href)
			if resolved == "" {
				return
			}
			pageType := urlutil.DetectPageType(resolved)
			if pageType == urlutil.PageTypeNonJob {
				return
			}
			if _, ok := seen[resolved]; ok {
				return
			}
			seen[resolved] = struct{}{}
			links = append(links, resolved)
		})
	}); err != nil {
		observability.IncError(observability.ClassifyFetchError(err), "scraper_generic")
		return links
	}
	observability.IncPagesCrawled("scraper_generic")
	return links
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
			if job := parseJSONLDString(e.Text); job != nil {
				jsonJob = job
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

func parseJSONLDString(raw string) *RawJob {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var payload interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	return findJobPosting(payload)
}

func findJobPosting(payload interface{}) *RawJob {
	switch t := payload.(type) {
	case map[string]interface{}:
		if job := jobFromMap(t); job != nil {
			return job
		}
		if graph, ok := t["@graph"].([]interface{}); ok {
			for _, item := range graph {
				if job := findJobPosting(item); job != nil {
					return job
				}
			}
		}
	case []interface{}:
		for _, item := range t {
			if job := findJobPosting(item); job != nil {
				return job
			}
		}
	}
	return nil
}

func jobFromMap(payload map[string]interface{}) *RawJob {
	if !isJobPostingType(payload["@type"]) {
		return nil
	}

	job := &RawJob{
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

func stringField(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case map[string]interface{}:
		if val, ok := t["@value"]; ok {
			if str, ok2 := val.(string); ok2 {
				return strings.TrimSpace(str)
			}
		}
	}
	return ""
}

func isJobPostingType(t interface{}) bool {
	switch v := t.(type) {
	case string:
		return v == "JobPosting"
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s == "JobPosting" {
				return true
			}
		}
	}
	return false
}

func orgName(v interface{}) string {
	if name := stringField(v); name != "" {
		return name
	}
	if org, ok := v.(map[string]interface{}); ok {
		return stringField(org["name"])
	}
	return ""
}

func parseLocation(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []interface{}:
		for _, item := range t {
			if loc := parseLocation(item); loc != "" {
				return loc
			}
		}
	case map[string]interface{}:
		if addr, ok := t["address"].(map[string]interface{}); ok {
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

func parseDate(v interface{}) time.Time {
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
