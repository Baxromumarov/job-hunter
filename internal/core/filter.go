package core

import "strings"

func MatchesKeywords(text string, keywords []string) bool {
	lowerText := strings.ToLower(text)
	for _, k := range keywords {
		if strings.Contains(lowerText, strings.ToLower(k)) {
			return true
		}
	}
	return false
}
