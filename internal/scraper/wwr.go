package scraper

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type WWRscraper struct {
	client *http.Client
}

func NewWWRScraper() *WWRscraper {
	return &WWRscraper{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (w *WWRscraper) FetchJobs(since time.Time) ([]RawJob, error) {
	url := "https://weworkremotely.com/categories/remote-programming-jobs"
	resp, err := w.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("wwr fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wwr status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wwr parse failed: %w", err)
	}

	var jobs []RawJob
	doc.Find("section.jobs article ul li a").Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists || href == "" {
			return
		}
		title := strings.TrimSpace(s.Find("span.title").Text())
		company := strings.TrimSpace(s.Find("span.company").Text())
		if title == "" || company == "" {
			return
		}
		jobURL := strings.TrimPrefix(href, "//")
		if !strings.HasPrefix(jobURL, "http") {
			jobURL = "https://weworkremotely.com" + href
		}

		jobs = append(jobs, RawJob{
			URL:         jobURL,
			Title:       title,
			Description: title + " at " + company,
			Company:     company,
			Location:    "Remote",
			PostedAt:    time.Time{},
		})
	})

	// Filter recent posts (they don't expose exact dates on the list; keep them all)
	filtered := make([]RawJob, 0, len(jobs))
	for _, j := range jobs {
		if !j.PostedAt.IsZero() && j.PostedAt.Before(since) {
			continue
		}
		filtered = append(filtered, j)
	}
	return filtered, nil
}
