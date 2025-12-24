package scraper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type leverPosting struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	HostedURL   string   `json:"hostedUrl"`
	Categories  category `json:"categories"`
	CreatedAt   int64    `json:"createdAt"`
	Description string   `json:"descriptionPlain"`
}

type category struct {
	Team     string `json:"team"`
	Location string `json:"location"`
}

type LeverScraper struct {
	client *http.Client
	base   string
}

func NewLeverScraper(baseURL string) *LeverScraper {
	return &LeverScraper{
		client: &http.Client{Timeout: 15 * time.Second},
		base:   baseURL,
	}
}

func (l *LeverScraper) FetchJobs(since time.Time) ([]RawJob, error) {
	parsed, err := url.Parse(l.base)
	if err != nil {
		return nil, fmt.Errorf("lever parse url failed: %w", err)
	}
	if strings.Trim(parsed.Path, "/") == "" {
		// Skip platform root pages that do not represent a specific company board.
		return nil, nil
	}

	apiURL := strings.TrimSuffix(l.base, "/")
	if !strings.Contains(apiURL, "?mode=json") {
		apiURL += "?mode=json"
	}

	resp, err := l.client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("lever fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lever status %d", resp.StatusCode)
	}

	var postings []leverPosting
	if err := json.NewDecoder(resp.Body).Decode(&postings); err != nil {
		return nil, fmt.Errorf("lever decode failed: %w", err)
	}

	var jobs []RawJob
	for _, p := range postings {
		posted := time.UnixMilli(p.CreatedAt)
		if posted.Before(since) {
			continue
		}
		jobs = append(jobs, RawJob{
			URL:         p.HostedURL,
			Title:       p.Text,
			Description: p.Description,
			Company:     companyFromLeverURL(p.HostedURL),
			Location:    p.Categories.Location,
			PostedAt:    posted,
		})
	}
	return jobs, nil
}

func companyFromLeverURL(u string) string {
	parts := strings.Split(u, "/")
	for i, p := range parts {
		if p == "lever.co" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "Lever"
}
