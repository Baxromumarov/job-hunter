package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"
	defaultModel  = "gemini-1.5-flash"
)

// GeminiClient implements the Client interface using Google's Gemini API.
// Free tier: 1,500 requests/day for gemini-1.5-flash
// Get your API key at: https://aistudio.google.com/apikey
type GeminiClient struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewGeminiClient creates a new Gemini AI client.
func NewGeminiClient(apiKey string) *GeminiClient {
	return &GeminiClient{
		apiKey: apiKey,
		model:  defaultModel,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WithModel allows changing the model (e.g., "gemini-1.5-pro")
func (g *GeminiClient) WithModel(model string) *GeminiClient {
	g.model = model
	return g
}

type geminiRequest struct {
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	Temperature      float64 `json:"temperature"`
	MaxOutputTokens  int     `json:"maxOutputTokens"`
	ResponseMIMEType string  `json:"responseMimeType,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

func (g *GeminiClient) callAPI(ctx context.Context, prompt string) (string, error) {
	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiBaseURL, g.model, g.apiKey)

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
		GenerationConfig: geminiGenerationConfig{
			Temperature:      0.1, // Low temperature for consistent JSON output
			MaxOutputTokens:  500,
			ResponseMIMEType: "application/json",
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if geminiResp.Error != nil {
		return "", fmt.Errorf("Gemini API error: %s (code: %d)", geminiResp.Error.Message, geminiResp.Error.Code)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from Gemini")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

// ClassifyWebsite uses Gemini to classify if a webpage is a job listing source.
func (g *GeminiClient) ClassifyWebsite(ctx context.Context, data WebsiteData) (WebsiteClassification, error) {
	prompt := fmt.Sprintf(`You are a strict website classifier.

Given a webpage snapshot, classify whether this page is a job listing source.

Return JSON only with this exact structure:
{
  "is_job_site": boolean,
  "tech_related": boolean,
  "confidence": number between 0 and 1,
  "reason": "short explanation"
}

Rules:
- "is_job_site" = true if the page contains job postings or career listings
- "tech_related" = true if jobs are software, engineering, IT, or tech-related
- "confidence" from 0 to 1 (how confident you are)
- "reason" = short explanation (max 50 words)

Webpage data:
URL: %s
Title: %s
Meta description: %s
Text sample:
%s`, data.URL, data.Title, data.MetaDescription, truncateText(data.TextSample, 500))

	response, err := g.callAPI(ctx, prompt)
	if err != nil {
		return WebsiteClassification{}, err
	}

	var result WebsiteClassification
	if err := json.Unmarshal([]byte(cleanJSON(response)), &result); err != nil {
		return WebsiteClassification{}, fmt.Errorf("failed to parse classification: %w (response: %s)", err, response)
	}

	return result, nil
}

// MatchJob uses Gemini to score how well a job matches a candidate profile.
func (g *GeminiClient) MatchJob(ctx context.Context, job JobData, profile CandidateProfile) (JobMatch, error) {
	techStack := strings.Join(profile.TechStack, ", ")

	prompt := fmt.Sprintf(`You are a job matching assistant.

Analyze how well this job matches the candidate's profile and return a JSON score.

Return JSON only with this exact structure:
{
  "match_score": number from 0 to 100,
  "strengths": ["strength1", "strength2"],
  "weaknesses": ["weakness1"],
  "short_summary": "one sentence summary"
}

Rules:
- match_score: 90-100 = excellent fit, 70-89 = good fit, 50-69 = partial fit, below 50 = poor fit
- Focus on Go/Golang, backend, and the candidate's tech stack
- strengths: what makes this a good match (max 3 items)
- weaknesses: what might be missing (max 2 items)
- short_summary: one sentence (max 20 words)

Candidate's Tech Stack: %s

Job Title: %s

Job Description:
%s`, techStack, job.Title, truncateText(job.Description, 800))

	response, err := g.callAPI(ctx, prompt)
	if err != nil {
		return JobMatch{}, err
	}

	var result JobMatch
	if err := json.Unmarshal([]byte(cleanJSON(response)), &result); err != nil {
		return JobMatch{}, fmt.Errorf("failed to parse match result: %w (response: %s)", err, response)
	}

	return result, nil
}

// truncateText limits text to maxLen characters
func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

// cleanJSON removes markdown code blocks if present
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
