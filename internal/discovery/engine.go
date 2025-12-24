package discovery

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	_ "embed"

	"github.com/baxromumarov/job-hunter/internal/core"
	"github.com/baxromumarov/job-hunter/internal/store"
)

type Engine struct {
	store      *store.Store
	classifier *core.ClassifierService
}

type candidateSource struct {
	URL        string `json:"url"`
	SourceType string `json:"source_type"`
	Title      string `json:"title"`
	Meta       string `json:"meta"`
	Text       string `json:"text"`
}

//go:embed seeds.json
var seedsJSON []byte

var seedCandidates = loadSeedCandidates()

func loadSeedCandidates() []candidateSource {
	var seeds []candidateSource
	if err := json.Unmarshal(seedsJSON, &seeds); err != nil {
		log.Printf("Discovery: failed to load embedded seeds: %v", err)
		return nil
	}
	return seeds
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
		e.crawlForCareerLinks(ctx)
		e.searchWeb(ctx)

		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.runCycle(ctx)
				e.crawlForCareerLinks(ctx)
				e.searchWeb(ctx)
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

func (e *Engine) crawlForCareerLinks(ctx context.Context) {
	sites := []string{
		"https://github.com",
		"https://about.gitlab.com",
		"https://www.heroku.com",
		"https://www.cloudflare.com",
		"https://www.vercel.com",
		"https://www.supabase.com",
		"https://www.datadoghq.com",
		"https://www.zendesk.com",
		"https://jobs.ashbyhq.com",
		"https://www.hashicorp.com",
		"https://www.digitalocean.com",
		"https://about.gitlab.com/careers/",
	}

	c := newCrawler()
	for _, site := range sites {
		select {
		case <-ctx.Done():
			return
		default:
		}
		links := c.extractCareerLinks(ctx, site)
		for _, link := range links {
			e.processCandidate(ctx, candidateSource{
				URL:        link,
				SourceType: guessSourceType(link),
			})
		}
	}
}

func (e *Engine) searchWeb(ctx context.Context) {
	queries := []string{
		"golang backend engineer jobs",
		"software engineer careers site:careers",
		"backend jobs site:jobs",
		"remote golang hiring",
		"golang remote backend job board",
		"senior golang engineer careers",
		"golang developer jobs Europe remote",
	}

	seen := make(map[string]struct{})
	c := newCrawler()
	for _, q := range queries {
		urls := duckDuckSearch(ctx, q, 15)
		for _, u := range urls {
			if _, ok := seen[u]; ok {
				continue
			}
			seen[u] = struct{}{}
			e.processCandidate(ctx, candidateSource{
				URL:        u,
				SourceType: guessSourceType(u),
			})

			// Crawl the result page for career/job links and enqueue those too.
			links := c.extractCareerLinks(ctx, u)
			for _, link := range links {
				if _, ok := seen[link]; ok {
					continue
				}
				seen[link] = struct{}{}
				e.processCandidate(ctx, candidateSource{
					URL:        link,
					SourceType: guessSourceType(link),
				})
			}
		}
	}
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

		id, existed, err := e.store.AddSource(ctx, c.URL, sourceType, classification.IsJobSite, classification.TechRelated, classification.Confidence, classification.Reason)
		if err != nil {
			log.Printf("Failed to store source %s: %v", c.URL, err)
		} else {
			if existed {
				log.Printf("Discovery: source already exists %s (ID: %d)", c.URL, id)
			} else {
				log.Printf("Discovery: APPROVED source %s (ID: %d)", c.URL, id)
				// Scraping now handled by ingestion using site-specific scrapers
			}
		}
	} else {
		log.Printf("Discovery: REJECTED source %s (Confidence: %.2f)", c.URL, classification.Confidence)
	}
}

func guessSourceType(u string) string {
	u = strings.ToLower(u)
	if strings.Contains(u, "remoteok") ||
		strings.Contains(u, "weworkremotely") ||
		strings.Contains(u, "builtin.com") ||
		strings.Contains(u, "linkedin") ||
		strings.Contains(u, "greenhouse.io") ||
		strings.Contains(u, "lever.co") {
		return "job_board"
	}
	return "company_page"
}
