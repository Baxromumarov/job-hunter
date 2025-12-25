package content

import (
	"encoding/json"
	"strings"
)

func hasJobPostingJSONLD(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return false
	}
	return containsJobPosting(payload)
}

func containsJobPosting(payload any) bool {
	switch t := payload.(type) {
	case map[string]any:
		if isJobPostingType(t["@type"]) {
			return true
		}
		if graph, ok := t["@graph"].([]any); ok {
			for _, item := range graph {
				if containsJobPosting(item) {
					return true
				}
			}
		}
	case []any:
		for _, item := range t {
			if containsJobPosting(item) {
				return true
			}
		}
	}
	return false
}

func isJobPostingType(t any) bool {
	switch v := t.(type) {
	case string:
		return v == "JobPosting"
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == "JobPosting" {
				return true
			}
		}
	}
	return false
}
