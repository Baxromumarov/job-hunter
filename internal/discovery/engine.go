package discovery

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/baxromumarov/job-hunter/internal/core"
	"github.com/baxromumarov/job-hunter/internal/store"
)

type Engine struct {
	store      *store.Store
	classifier *core.ClassifierService
}

type candidateSource struct {
	URL        string
	SourceType string
	Title      string
	Meta       string
	Text       string
}

var seedCandidates = []candidateSource{
	{URL: "https://remoteok.com/remote-dev-jobs", SourceType: "job_board", Title: "RemoteOK programming jobs", Meta: "Remote developer jobs", Text: "Remote OK lists software engineering, backend and golang roles posted daily."},
	{URL: "https://weworkremotely.com/categories/remote-programming-jobs", SourceType: "job_board", Title: "WeWorkRemotely programming jobs", Meta: "Remote programming jobs", Text: "Remote programming and backend roles including Go developers."},
	{URL: "https://www.linkedin.com/jobs/search/?keywords=golang", SourceType: "job_board", Title: "LinkedIn Golang search", Meta: "Golang backend roles", Text: "LinkedIn search results for Golang backend engineer positions."},
	{URL: "https://arc.dev/remote-software-jobs", SourceType: "job_board", Title: "Arc remote software jobs", Meta: "Remote software engineer jobs", Text: "Remote backend and Go jobs curated for engineers."},
	{URL: "https://jobs.lever.co/airbnb", SourceType: "company_page", Title: "Airbnb careers", Meta: "Join Airbnb engineering", Text: "Airbnb engineering teams hiring backend and infrastructure roles."},
	{URL: "https://boards.greenhouse.io/stripe", SourceType: "company_page", Title: "Stripe careers", Meta: "Stripe is hiring engineers", Text: "Stripe engineering and backend services roles."},
	{URL: "https://vercel.com/careers", SourceType: "company_page", Title: "Vercel Careers", Meta: "Work on Vercel platform", Text: "Hiring backend, Go, and platform engineers."},
	{URL: "https://supabase.com/careers", SourceType: "company_page", Title: "Supabase Careers", Meta: "Join Supabase", Text: "Database and backend engineering positions."},
	{URL: "https://jobs.github.com/positions", SourceType: "job_board", Title: "GitHub Jobs", Meta: "Software engineering roles", Text: "Open software engineering positions including Go developers."},
	{URL: "https://builtin.com/jobs", SourceType: "job_board", Title: "BuiltIn tech jobs", Meta: "Tech job board", Text: "Tech job board with backend, Go, and cloud roles."},
	{URL: "https://lever.co", SourceType: "job_board", Title: "Lever hosted boards", Meta: "Career platform", Text: "Job listings platform commonly used for careers pages."},
	{URL: "https://greenhouse.io", SourceType: "job_board", Title: "Greenhouse job boards", Meta: "Career platform", Text: "Career boards for many startups and tech companies."},
}

func NewEngine(store *store.Store, classifier *core.ClassifierService) *Engine {
	return &Engine{
		store:      store,
		classifier: classifier,
	}
}

func (e *Engine) StartDiscovery(ctx context.Context) {
	log.Println("Starting Auto Discovery Engine...")

	go func() {
		e.runCycle(ctx)

		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.runCycle(ctx)
			}
		}
	}()
}

func (e *Engine) runCycle(ctx context.Context) {
	for _, c := range seedCandidates {
		select {
		case <-ctx.Done():
			return
		default:
			e.processCandidate(ctx, c)
			time.Sleep(500 * time.Millisecond)
		}
	}
	log.Println("Discovery: cycle complete")
}

func (e *Engine) processCandidate(ctx context.Context, c candidateSource) {
	log.Printf("Discovery: Analyzing candidate %s", c.URL)

	// In a real scenario, we would fetch the page content here.
	// We pass dummy content to the classifier, which mocks the AI anyway.
	// If we had a real scraper, we'd use it here.

	title := c.Title
	if title == "" {
		title = "Title for " + c.URL
	}
	meta := c.Meta
	if meta == "" {
		meta = "Meta for " + c.URL
	}
	text := c.Text
	if text == "" {
		text = "Sample text for " + c.URL + " which is definitely long enough to pass the fifty character limit set in the mock ai client."
	}

	classification, err := e.classifier.Classify(ctx, c.URL, title, meta, text)
	if err != nil {
		log.Printf("Failed to classify %s: %v", c.URL, err)
		return
	}

	if classification.IsJobSite && classification.TechRelated && classification.Confidence > 0.7 {
		sourceType := c.SourceType
		if sourceType == "" {
			sourceType = guessSourceType(c.URL)
		}

		id, err := e.store.AddSource(ctx, c.URL, sourceType, classification.IsJobSite, classification.TechRelated, classification.Confidence, classification.Reason)
		if err != nil {
			log.Printf("Failed to store source %s: %v", c.URL, err)
		} else {
			log.Printf("Discovery: APPROVED source %s (ID: %d)", c.URL, id)
			// Trigger job scraping for this new source (Mocked for now)
			e.mockScrapeJobs(ctx, id, c)
		}
	} else {
		log.Printf("Discovery: REJECTED source %s (Confidence: %.2f)", c.URL, classification.Confidence)
	}
}

func (e *Engine) mockScrapeJobs(ctx context.Context, sourceID int, c candidateSource) {
	// Determine company name from URL
	company := deriveCompanyName(c.URL)

	// Mock Keyword Filtering
	// In a real scraper, we would check the scraped description text.
	// Here we simulate it by only "saving" if the mock data we generate contains keywords.
	// For this demo, let's say "wikipedia" or "stackoverflow" don't match our tech stack

	title := "Senior Software Engineer"
	description := "We are hiring for " + company + ". Must know Golang and Backend systems."

	// Simple keyword filter logic
	if !core.MatchesKeywords(description+" "+title, []string{"go", "golang", "backend", "software engineer"}) {
		log.Printf("Discovery: Filtered out job from %s due to keyword mismatch", company)
		return
	}

	postDate := time.Now()
	job := store.Job{
		SourceID:     sourceID,
		SourceURL:    c.URL,
		SourceType:   c.SourceType,
		URL:          c.URL + "/job/auto-1",
		Title:        title,
		Description:  description,
		Company:      company,
		Location:     "Remote",
		MatchScore:   85,
		MatchSummary: "Auto-matched by AI",
		PostedAt:     &postDate,
	}

	if err := e.store.SaveJob(ctx, job); err != nil {
		log.Printf("Failed to save mock job: %v", err)
	} else {
		log.Printf("Discovery: Scraped and saved job for %s", company)
	}
}

func deriveCompanyName(url string) string {
	trimmed := strings.TrimPrefix(url, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	parts := strings.Split(trimmed, ".")
	if len(parts) > 0 && parts[0] != "" {
		name := parts[0]
		return strings.ToUpper(name[:1]) + name[1:]
	}
	return "Unknown"
}

func guessSourceType(url string) string {
	if strings.Contains(url, "remoteok") || strings.Contains(url, "weworkremotely") || strings.Contains(url, "builtin.com") || strings.Contains(url, "linkedin") {
		return "job_board"
	}
	if strings.Contains(url, "greenhouse") || strings.Contains(url, "lever") {
		return "job_board"
	}
	return "company_page"
}
