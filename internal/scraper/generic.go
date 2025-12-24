package scraper

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

type GenericScraper struct {
	BaseURL string
	client  *http.Client
}

func NewGenericScraper(baseURL string) *GenericScraper {
	return &GenericScraper{
		BaseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *GenericScraper) FetchJobs(since time.Time) ([]RawJob, error) {
	resp, err := s.client.Get(s.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", s.BaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received status %d from %s", resp.StatusCode, s.BaseURL)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", s.BaseURL, err)
	}

	base, _ := url.Parse(s.BaseURL)
	seen := make(map[string]struct{})
	var jobs []RawJob

	doc.Find("a").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok || href == "" {
			return
		}

		lower := strings.ToLower(href + " " + strings.TrimSpace(a.Text()))
		if !strings.Contains(lower, "job") && !strings.Contains(lower, "career") && !strings.Contains(lower, "opening") {
			return
		}

		resolved := resolveLink(base, href)
		if resolved == "" {
			return
		}
		if _, exists := seen[resolved]; exists {
			return
		}

		title := strings.TrimSpace(a.Text())
		if title == "" {
			title = pathTitleFromURL(resolved)
		}
		if title == "" {
			return
		}

		seen[resolved] = struct{}{}
		jobs = append(jobs, RawJob{
			URL:         resolved,
			Title:       title,
			Description: title,
			Company:     hostCompany(base),
			Location:    "Remote/Unknown",
			PostedAt:    time.Now(),
		})
	})

	// keep the most recent subset to avoid flooding
	if len(jobs) > 20 {
		jobs = jobs[:20]
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
