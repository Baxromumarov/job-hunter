package scraper

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type AshbyScraper struct {
	client *http.Client
	base   string
}

type ashbyAppData struct {
	Organization *struct {
		Name string `json:"name"`
	} `json:"organization"`
	JobBoard *struct {
		JobPostings []ashbyJobPosting `json:"jobPostings"`
	} `json:"jobBoard"`
}

type ashbyJobPosting struct {
	ID             string `json:"id"`
	JobID          string `json:"jobId"`
	Title          string `json:"title"`
	LocationName   string `json:"locationName"`
	WorkplaceType  string `json:"workplaceType"`
	EmploymentType string `json:"employmentType"`
	PublishedDate  string `json:"publishedDate"`
	UpdatedAt      string `json:"updatedAt"`
	TeamName       string `json:"teamName"`
	DepartmentName string `json:"departmentName"`
	IsListed       bool   `json:"isListed"`
}

func NewAshbyScraper(baseURL string) *AshbyScraper {
	return &AshbyScraper{
		client: &http.Client{Timeout: 15 * time.Second},
		base:   baseURL,
	}
}

func (a *AshbyScraper) FetchJobs(since time.Time) ([]RawJob, error) {
	parsed, err := url.Parse(a.base)
	if err != nil {
		return nil, fmt.Errorf("ashby parse url failed: %w", err)
	}
	if strings.Trim(parsed.Path, "/") == "" {
		// Skip platform root pages that are not a company board.
		return nil, nil
	}

	resp, err := a.client.Get(a.base)
	if err != nil {
		return nil, fmt.Errorf("ashby fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ashby status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ashby read failed: %w", err)
	}

	dataJSON, err := extractAshbyAppData(body)
	if err != nil {
		return nil, fmt.Errorf("ashby appdata parse failed: %w", err)
	}

	var app ashbyAppData
	if err := json.Unmarshal(dataJSON, &app); err != nil {
		return nil, fmt.Errorf("ashby appdata decode failed: %w", err)
	}
	if app.JobBoard == nil || len(app.JobBoard.JobPostings) == 0 {
		return nil, nil
	}

	company := strings.TrimSpace(strings.Trim(parsed.Path, "/"))
	if app.Organization != nil && strings.TrimSpace(app.Organization.Name) != "" {
		company = strings.TrimSpace(app.Organization.Name)
	}

	baseURL := strings.TrimSuffix(a.base, "/")
	seen := make(map[string]struct{})
	var jobs []RawJob

	for _, posting := range app.JobBoard.JobPostings {
		if !posting.IsListed {
			continue
		}
		jobID := posting.JobID
		if jobID == "" {
			jobID = posting.ID
		}
		if jobID == "" || posting.Title == "" {
			continue
		}
		if _, ok := seen[jobID]; ok {
			continue
		}
		seen[jobID] = struct{}{}

		posted := parseAshbyDate(posting.PublishedDate)
		if posted.IsZero() {
			posted = parseAshbyDate(posting.UpdatedAt)
		}
		if !posted.IsZero() && posted.Before(since) {
			continue
		}

		loc := strings.TrimSpace(posting.LocationName)
		if posting.WorkplaceType != "" && !strings.Contains(strings.ToLower(loc), strings.ToLower(posting.WorkplaceType)) {
			if loc == "" {
				loc = posting.WorkplaceType
			} else {
				loc = loc + " (" + posting.WorkplaceType + ")"
			}
		}

		descParts := []string{posting.Title}
		if posting.DepartmentName != "" {
			descParts = append(descParts, posting.DepartmentName)
		}
		if posting.TeamName != "" {
			descParts = append(descParts, posting.TeamName)
		}
		if posting.EmploymentType != "" {
			descParts = append(descParts, posting.EmploymentType)
		}
		description := strings.Join(descParts, " - ")

		jobs = append(jobs, RawJob{
			URL:         baseURL + "/" + jobID,
			Title:       posting.Title,
			Description: description,
			Company:     company,
			Location:    loc,
			PostedAt:    posted,
		})
	}

	return jobs, nil
}

func extractAshbyAppData(body []byte) ([]byte, error) {
	marker := []byte("window.__appData")
	idx := bytes.Index(body, marker)
	if idx == -1 {
		return nil, errors.New("appdata marker not found")
	}
	start := bytes.IndexByte(body[idx+len(marker):], '{')
	if start == -1 {
		return nil, errors.New("appdata json start not found")
	}
	start += idx + len(marker)

	return extractJSONObject(body, start)
}

func extractJSONObject(body []byte, start int) ([]byte, error) {
	depth := 0
	inString := false
	escape := false

	for i := start; i < len(body); i++ {
		c := body[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}

		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return body[start : i+1], nil
			}
		}
	}

	return nil, errors.New("appdata json end not found")
}

func parseAshbyDate(val string) time.Time {
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
