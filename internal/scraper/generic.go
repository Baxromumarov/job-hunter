package scraper

import (
	"fmt"
	"net/http"
	"strings"
	"time"

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
	// This is a placeholder implementation that would normally parse the specific site structure
	// For now, we will simulated returning a job if we can connect to the URL

	resp, err := s.client.Get(s.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", s.BaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received status %d from %s", resp.StatusCode, s.BaseURL)
	}

	// In a real implementation, we would parse the HTML response body here
	// using goquery or html package.
	// Returning a dummy job for demonstration purposes.

	jobs := []RawJob{
		{
			URL:         s.BaseURL + "/job/123",
			Title:       "Software Engineer",
			Description: "We are looking for a Go developer...",
			Company:     "Example Corp",
			Location:    "Remote",
			PostedAt:    time.Now(),
		},
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
