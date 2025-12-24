package discovery

import (
	"context"
	"net/url"
	"strings"

	"github.com/baxromumarov/job-hunter/internal/httpx"
	"github.com/gocolly/colly/v2"
)

// duckDuckSearch fetches a small set of URLs from DuckDuckGo html endpoint for a query.
func duckDuckSearch(ctx context.Context, query string, limit int) []string {
	fetcher := httpx.NewCollyFetcher("job-hunter-bot/1.0")
	reqURL := "https://duckduckgo.com/html/?q=" + url.QueryEscape(query)

	var urls []string
	_ = fetcher.Fetch(ctx, reqURL, func(c *colly.Collector) {
		c.OnHTML("a[href]", func(e *colly.HTMLElement) {
			if limit > 0 && len(urls) >= limit {
				e.Request.Abort()
				return
			}
			href := e.Attr("href")
			if href == "" {
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
