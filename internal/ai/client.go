package ai

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"
)

type Client interface {
	ClassifyWebsite(ctx context.Context, data WebsiteData) (WebsiteClassification, error)
	MatchJob(ctx context.Context, job JobData, profile CandidateProfile) (JobMatch, error)
}

// NewClient creates an AI client based on the AI_PROVIDER environment variable.
// Supported providers: "gemini" (default if GEMINI_API_KEY is set), "mock"
//
// Environment variables:
//   - AI_PROVIDER: "gemini" or "mock" (optional, auto-detected)
//   - GEMINI_API_KEY: Your Google Gemini API key (get free at https://aistudio.google.com/apikey)
func NewClient() Client {
	provider := strings.ToLower(os.Getenv("AI_PROVIDER"))
	geminiKey := os.Getenv("GEMINI_API_KEY")

	// Auto-detect provider if not specified
	if provider == "" {
		if geminiKey != "" {
			provider = "gemini"
		} else {
			provider = "mock"
		}
	}

	switch provider {
	case "gemini":
		if geminiKey == "" {
			fmt.Println("WARNING: AI_PROVIDER=gemini but GEMINI_API_KEY not set, falling back to mock")
			return NewMockClient()
		}
		fmt.Println("Using Gemini AI client (free tier: 1,500 requests/day)")
		return NewGeminiClient(geminiKey)
	default:
		fmt.Println("Using Mock AI client (set GEMINI_API_KEY for real AI)")
		return NewMockClient()
	}
}

type WebsiteData struct {
	URL             string
	Title           string
	MetaDescription string
	TextSample      string
}

type WebsiteClassification struct {
	IsJobSite   bool    `json:"is_job_site"`
	TechRelated bool    `json:"tech_related"`
	Confidence  float64 `json:"confidence"`
	Reason      string  `json:"reason"`
}

type JobData struct {
	Title       string
	Description string
}

type CandidateProfile struct {
	TechStack []string
}

type JobMatch struct {
	MatchScore   int      `json:"match_score"`
	Strengths    []string `json:"strengths"`
	Weaknesses   []string `json:"weaknesses"`
	ShortSummary string   `json:"short_summary"`
}

type MockClient struct{}

func NewMockClient() *MockClient {
	return &MockClient{}
}

func (m *MockClient) ClassifyWebsite(ctx context.Context, data WebsiteData) (WebsiteClassification, error) {
	// Simulate AI delay
	time.Sleep(500 * time.Millisecond)

	// Simple heuristic for mock
	isJob := len(data.TextSample) > 50
	isTech := true

	return WebsiteClassification{
		IsJobSite:   isJob,
		TechRelated: isTech,
		Confidence:  0.8 + (rand.Float64() * 0.2),
		Reason:      "Mock AI analysis determined this is likely a job site.",
	}, nil
}

func (m *MockClient) MatchJob(ctx context.Context, job JobData, profile CandidateProfile) (JobMatch, error) {
	time.Sleep(500 * time.Millisecond)
	score := 70 + rand.Intn(30)
	return JobMatch{
		MatchScore:   score,
		Strengths:    []string{"Golang", "Backend"},
		Weaknesses:   []string{"Unknown stack"},
		ShortSummary: fmt.Sprintf("Mock match score: %d", score),
	}, nil
}
