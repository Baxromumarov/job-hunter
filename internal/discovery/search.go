package discovery

import (
	"context"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/baxromumarov/job-hunter/internal/httpx"
)

// duckDuckSearch fetches a small set of URLs from DuckDuckGo html endpoint for a query.
func duckDuckSearch(ctx context.Context, query string, limit int) []string {
	client := httpx.NewPoliteClient("job-hunter-bot/1.0")
	reqURL := "https://duckduckgo.com/html/?q=" + url.QueryEscape(query)

	req, err := httpx.NewRequest(ctx, reqURL)
	if err != nil {
		return nil
	}

	resp, err := client.Do(ctx, req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil
	}

	var urls []string
	doc.Find("a").Each(func(_ int, a *goquery.Selection) {
		if limit > 0 && len(urls) >= limit {
			return
		}
		href, ok := a.Attr("href")
		if !ok || href == "" {
			return
		}

		// DuckDuckGo rewrites links as /l/?uddg=<encoded>
		if strings.Contains(href, "duckduckgo.com/l/?uddg=") {
			if decoded := decodeDDGLink(href); decoded != "" {
				href = decoded
			}
		}

		if !strings.HasPrefix(href, "http") {
			return
		}
		if strings.Contains(href, "duckduckgo.com") {
			return
		}

		urls = append(urls, href)
	})

	return urls
}

func decodeDDGLink(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if v := u.Query().Get("uddg"); v != "" {
		decoded, err := url.QueryUnescape(v)
		if err == nil {
			return decoded
		}
	}
	return ""
}
