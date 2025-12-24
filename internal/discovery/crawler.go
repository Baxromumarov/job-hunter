package discovery

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// crawler fetches known tech/startup pages and looks for career links.
type crawler struct {
	client *http.Client
}

func newCrawler() *crawler {
	return &crawler{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *crawler) extractCareerLinks(ctx context.Context, url string) []string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	var links []string
	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || href == "" {
			return
		}
		lower := strings.ToLower(href)
		if !strings.Contains(lower, "career") && !strings.Contains(lower, "jobs") {
			return
		}
		if strings.HasPrefix(href, "/") {
			parts := strings.Split(url, "/")
			base := parts[0] + "//" + parts[2]
			href = base + href
		}
		if !strings.HasPrefix(href, "http") {
			return
		}
		if _, ok := seen[href]; ok {
			return
		}
		seen[href] = struct{}{}
		links = append(links, href)
	})
	return links
}
