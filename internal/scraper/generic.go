package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/baxromumarov/job-hunter/internal/httpx"
	"golang.org/x/net/html"
)

type GenericScraper struct {
	BaseURL string
	client  *httpx.PoliteClient
}

func NewGenericScraper(baseURL string) *GenericScraper {
	return &GenericScraper{
		BaseURL: baseURL,
		client:  httpx.NewPoliteClient("job-hunter-bot/1.0"),
	}
}

func (s *GenericScraper) FetchJobs(since time.Time) ([]RawJob, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	base, _ := url.Parse(s.BaseURL)

	rootDoc, err := s.fetchDoc(ctx, s.BaseURL)
	if err != nil {
		return nil, err
	}

	candidates := s.collectDetailLinks(rootDoc, base)
	for _, probe := range probePaths(base) {
		if len(candidates) >= 50 {
			break
		}
		if doc, err := s.fetchDoc(ctx, probe); err == nil {
			candidates = append(candidates, s.collectDetailLinks(doc, base)...)
		}
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

		doc, err := s.fetchDoc(ctx, link)
		if err != nil {
			continue
		}

		if job := s.extractJob(doc, link, base); job != nil {
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

func (s *GenericScraper) fetchDoc(ctx context.Context, link string) (*goquery.Document, error) {
	req, err := httpx.NewRequest(ctx, link)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

func (s *GenericScraper) collectDetailLinks(doc *goquery.Document, base *url.URL) []string {
	seen := make(map[string]struct{})
	var links []string
	doc.Find("a").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok || href == "" {
			return
		}
		lower := strings.ToLower(href + " " + strings.TrimSpace(a.Text()))
		if !strings.Contains(lower, "job") &&
			!strings.Contains(lower, "career") &&
			!strings.Contains(lower, "opening") &&
			!strings.Contains(lower, "position") {
			return
		}

		resolved := resolveLink(base, href)
		if resolved == "" {
			return
		}
		if _, ok := seen[resolved]; ok {
			return
		}
		seen[resolved] = struct{}{}
		links = append(links, resolved)
	})
	return links
}

func (s *GenericScraper) extractJob(doc *goquery.Document, link string, base *url.URL) *RawJob {
	if job := parseJSONLD(doc); job != nil {
		if job.URL == "" {
			job.URL = link
		}
		if job.Company == "" {
			job.Company = hostCompany(base)
		}
		return job
	}

	title := strings.TrimSpace(doc.Find("h1").First().Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("title").First().Text())
	}
	if title == "" {
		title = pathTitleFromURL(link)
	}
	if title == "" {
		return nil
	}

	desc := strings.TrimSpace(doc.Find("meta[name='description']").AttrOr("content", ""))
	if desc == "" {
		desc = strings.TrimSpace(doc.Find("p").First().Text())
	}

	return &RawJob{
		URL:         link,
		Title:       title,
		Description: desc,
		Company:     hostCompany(base),
		Location:    "",
	}
}

func parseJSONLD(doc *goquery.Document) *RawJob {
	var job RawJob
	found := false
	doc.Find("script[type='application/ld+json']").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return true
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return true
		}
		types := payload["@type"]
		switch t := types.(type) {
		case string:
			if t != "JobPosting" {
				return true
			}
		case []interface{}:
			ok := false
			for _, item := range t {
				if str, ok2 := item.(string); ok2 && str == "JobPosting" {
					ok = true
				}
			}
			if !ok {
				return true
			}
		default:
			return true
		}

		job.Title = stringField(payload["title"])
		job.Description = stringField(payload["description"])
		job.Company = stringField(payload["hiringOrganization"])
		if job.Company == "" {
			if org, ok := payload["hiringOrganization"].(map[string]interface{}); ok {
				job.Company = stringField(org["name"])
			}
		}
		job.Location = stringField(payload["jobLocation"])
		if loc, ok := payload["jobLocation"].(map[string]interface{}); ok {
			job.Location = stringField(loc["addressLocality"])
			if region := stringField(loc["addressRegion"]); region != "" {
				job.Location = strings.TrimSpace(job.Location + " " + region)
			}
		}

		if posted := stringField(payload["datePosted"]); posted != "" {
			if t, err := time.Parse(time.RFC3339, posted); err == nil {
				job.PostedAt = t
			}
		}
		found = true
		return false
	})

	if !found || job.Title == "" {
		return nil
	}
	return &job
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
