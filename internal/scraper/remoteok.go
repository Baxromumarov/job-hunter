package scraper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RemoteOK API returns a JSON array; the first element is metadata.
type remoteOKJob struct {
	Slug        string   `json:"slug"`
	Company     string   `json:"company"`
	Position    string   `json:"position"`
	URL         string   `json:"url"`
	Tags        []string `json:"tags"`
	Date        string   `json:"date"`
	Description string   `json:"description"`
	Location    string   `json:"location"`
}

type RemoteOKScraper struct {
	client *http.Client
	tag    string
}

func NewRemoteOKScraper(tag string) *RemoteOKScraper {
	if tag == "" {
		tag = "golang"
	}
	return &RemoteOKScraper{
		client: &http.Client{Timeout: 15 * time.Second},
		tag:    tag,
	}
}

func (r *RemoteOKScraper) FetchJobs(since time.Time) ([]RawJob, error) {
	resp, err := r.client.Get("https://remoteok.com/api")
	if err != nil {
		return nil, fmt.Errorf("remoteok fetch failed: %w", err)
	}
	defer resp.Body.Close()

	var data []remoteOKJob
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("remoteok decode failed: %w", err)
	}

	var jobs []RawJob
	for _, j := range data {
		// Skip metadata element
		if j.Slug == "" || j.URL == "" {
			continue
		}
		if !hasTag(j.Tags, r.tag) {
			continue
		}
		postedAt := parseRemoteOKDate(j.Date)
		if !postedAt.IsZero() && postedAt.Before(since) {
			continue
		}
		jobs = append(jobs, RawJob{
			URL:         j.URL,
			Title:       j.Position,
			Description: j.Description,
			Company:     j.Company,
			Location:    j.Location,
			PostedAt:    postedAt,
		})
	}
	return jobs, nil
}

func hasTag(tags []string, want string) bool {
	wantLower := strings.ToLower(want)
	for _, t := range tags {
		if strings.ToLower(t) == wantLower {
			return true
		}
	}
	return false
}

func parseRemoteOKDate(val string) time.Time {
	if val == "" {
		return time.Time{}
	}
	// Example: "2023-12-20T04:02:19+00:00"
	t, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return time.Time{}
	}
	return t
}
