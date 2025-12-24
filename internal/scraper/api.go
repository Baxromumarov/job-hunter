package scraper

import (
	"time"
)

type RawJob struct {
	URL         string
	Title       string
	Description string
	Company     string
	Location    string
	PostedAt    time.Time
}

type JobScraper interface {
	FetchJobs(since time.Time) ([]RawJob, error)
}

type RelaxedScraper interface {
	FetchJobsRelaxed(since time.Time) ([]RawJob, error)
}

type Normalizer interface {
	Normalize(htmlContent string) (string, error)
}
