package scraper

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type GreenhouseScraper struct {
	client *http.Client
	base   string
}

func NewGreenhouseScraper(baseURL string) *GreenhouseScraper {
	return &GreenhouseScraper{
		client: &http.Client{Timeout: 15 * time.Second},
		base:   baseURL,
	}
}

func (g *GreenhouseScraper) FetchJobs(since time.Time) ([]RawJob, error) {
	parsed, err := url.Parse(g.base)
	if err != nil {
		return nil, fmt.Errorf("greenhouse parse url failed: %w", err)
	}
	if strings.Trim(parsed.Path, "/") == "" {
		// Skip platform root pages that are not a company board.
		return nil, nil
	}

	url := g.normalizeURL(g.base)
	resp, err := g.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("greenhouse fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("greenhouse status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("greenhouse parse failed: %w", err)
	}

	var jobs []RawJob
	doc.Find(".opening").Each(func(_ int, s *goquery.Selection) {
		link := s.Find("a")
		href, ok := link.Attr("href")
		if !ok || href == "" {
			return
		}
		title := strings.TrimSpace(link.Text())
		location := strings.TrimSpace(s.Find(".location").Text())
		jobURL := href
		if !strings.HasPrefix(jobURL, "http") {
			jobURL = "https://boards.greenhouse.io" + jobURL
		}
		jobs = append(jobs, RawJob{
			URL:         jobURL,
			Title:       title,
			Description: title,
			Company:     companyFromGreenhouse(jobURL),
			Location:    location,
			PostedAt:    time.Time{},
		})
	})

	filtered := make([]RawJob, 0, len(jobs))
	for _, j := range jobs {
		if !j.PostedAt.IsZero() && j.PostedAt.Before(since) {
			continue
		}
		filtered = append(filtered, j)
	}
	return filtered, nil
}

func (g *GreenhouseScraper) normalizeURL(base string) string {
	if strings.Contains(base, "embed") {
		return base
	}
	if strings.Contains(base, "boards.greenhouse.io") {
		return base + "/embed/jobs?content=true"
	}
	return base
}

func companyFromGreenhouse(u string) string {
	parts := strings.Split(u, "/")
	for i, p := range parts {
		if p == "boards.greenhouse.io" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "Greenhouse"
}
