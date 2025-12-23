package scraper

import (
	"strings"

	"golang.org/x/net/html"
)

type SimpleNormalizer struct{}

func NewSimpleNormalizer() *SimpleNormalizer {
	return &SimpleNormalizer{}
}

func (n *SimpleNormalizer) Normalize(htmlContent string) (string, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return "", err
	}

	// Extract text and clean up
	text := ExtractText(doc)
	normalized := strings.Join(strings.Fields(text), " ")
	return normalized, nil
}
