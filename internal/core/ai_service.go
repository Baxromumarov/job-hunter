package core

import (
	"context"
	"fmt"

	"github.com/baxromumarov/job-hunter/internal/ai"
	"github.com/baxromumarov/job-hunter/internal/observability"
)

type ClassifierService struct {
	aiClient ai.Client
}

func NewClassifierService(aiClient ai.Client) *ClassifierService {
	return &ClassifierService{aiClient: aiClient}
}

func (s *ClassifierService) Classify(ctx context.Context, url, title, meta, text string) (*ai.WebsiteClassification, error) {
	data := ai.WebsiteData{
		URL:             url,
		Title:           title,
		MetaDescription: meta,
		TextSample:      text,
	}

	observability.IncAICall("classifier")
	result, err := s.aiClient.ClassifyWebsite(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("classification failed: %w", err)
	}

	return &result, nil
}

type MatcherService struct {
	aiClient ai.Client
}

func NewMatcherService(aiClient ai.Client) *MatcherService {
	return &MatcherService{aiClient: aiClient}
}

func (s *MatcherService) Match(ctx context.Context, jobTitle, jobDesc string, profile ai.CandidateProfile) (*ai.JobMatch, error) {
	jobData := ai.JobData{
		Title:       jobTitle,
		Description: jobDesc,
	}

	observability.IncAICall("matcher")
	result, err := s.aiClient.MatchJob(ctx, jobData, profile)
	if err != nil {
		return nil, fmt.Errorf("matching failed: %w", err)
	}

	return &result, nil
}
