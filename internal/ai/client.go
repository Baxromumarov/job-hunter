package ai

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

type Client interface {
	ClassifyWebsite(ctx context.Context, data WebsiteData) (WebsiteClassification, error)
	MatchJob(ctx context.Context, job JobData, profile CandidateProfile) (JobMatch, error)
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
