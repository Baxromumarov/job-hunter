package discovery

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"

	"log/slog"

	"github.com/baxromumarov/job-hunter/internal/httpx"
	"github.com/baxromumarov/job-hunter/internal/observability"
	"github.com/baxromumarov/job-hunter/internal/urlutil"
	"github.com/gocolly/colly/v2"
)

// crawler fetches known tech/startup pages and looks for career links.
type crawler struct {
	fetcher *httpx.CollyFetcher
}

func newCrawler() *crawler {
	return &crawler{
		fetcher: httpx.NewCollyFetcher("job-hunter-bot/1.0"),
	}
}

// extractCareerLinks crawls a page and augments results with path probes, sitemaps, and ATS detections.
func (c *crawler) extractCareerLinks(ctx context.Context, rawURL string) []string {
	base, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	seenATS := make(map[string]struct{})
	var out []string
	var atsOut []string

	add := func(u string) {
		if u == "" || !strings.HasPrefix(u, "http") {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	addATS := func(u string) {
		if u == "" || !strings.HasPrefix(u, "http") {
			return
		}
		if _, ok := seenATS[u]; ok {
			return
		}
		seenATS[u] = struct{}{}
		atsOut = append(atsOut, u)
	}

	links, atsLinks, rateLimited := c.collectLinksFromPage(ctx, rawURL)
	if len(atsLinks) > 0 {
		for _, link := range atsLinks {
			addATS(link)
		}
		return atsOut
	}
	for _, link := range links {
		add(link)
	}
	if rateLimited {
		return out
	}
	if urlutil.IsATSHost(base.Hostname()) {
		return out
	}

	for _, probe := range probePaths(base) {
		if _, ok := seen[probe]; ok {
			continue
		}
		links, atsLinks, rateLimited = c.collectLinksFromPage(ctx, probe)
		if rateLimited {
			return out
		}
		add(probe)
		if len(atsLinks) > 0 {
			for _, link := range atsLinks {
				addATS(link)
			}
			return atsOut
		}
		for _, link := range links {
			add(link)
		}
	}

	for _, link := range parseSitemaps(ctx, c.fetcher, base) {
		if urlutil.IsATSHost(hostFromURL(link)) {
			addATS(link)
			continue
		}
		add(link)
	}
	if len(atsOut) > 0 {
		return atsOut
	}

	return out
}

func (c *crawler) collectLinksFromPage(ctx context.Context, target string) ([]string, []string, bool) {
	pageBase, err := url.Parse(target)
	if err != nil {
		return nil, nil, false
	}

	seen := make(map[string]struct{})
	var atsLinks []string
	var links []string
	if err := c.fetcher.Fetch(ctx, target, func(col *colly.Collector) {
		col.OnHTML("a[href]", func(e *colly.HTMLElement) {
			href := e.Attr("href")
			if href == "" {
				return
			}
			resolved := resolveLink(pageBase, href)
			if resolved == "" {
				return
			}
			normalized, host, err := urlutil.Normalize(resolved)
			if err != nil {
				return
			}
			if _, ok := seen[normalized]; ok {
				return
			}
			seen[normalized] = struct{}{}

			if urlutil.IsATSHost(host) {
				atsLinks = append(atsLinks, normalized)
				return
			}

			if !sameHost(pageBase, host) {
				return
			}

			if !urlutil.IsDiscoveryEligible(normalized) {
				return
			}

			links = append(links, normalized)
		})
	}); err != nil {
		observability.IncError(observability.ClassifyFetchError(err), "discovery")
		slog.Debug("discovery page fetch failed", "url", target, "error", err)
		return links, atsLinks, isRateLimited(err)
	}
	observability.IncPagesCrawled("discovery")

	return links, atsLinks, false
}

func probePaths(base *url.URL) []string {
	if base == nil {
		return nil
	}
	paths := []string{
		"/careers",
		"/jobs",
		"/careers/jobs",
		"/join-us",
		"/work-with-us",
		"/opportunities",
		"/teams",
		"/engineering",
		"/early-careers",
		"/company/careers",
	}
	var out []string
	for _, p := range paths {
		res := *base
		res.Path = path.Clean(p)
		out = append(out, res.String())
	}
	return out
}

type sitemapIndex struct {
	Locations []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

type urlset struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

func parseSitemaps(ctx context.Context, fetcher *httpx.CollyFetcher, base *url.URL) []string {
	if base == nil {
		return nil
	}
	candidates := []string{
		base.ResolveReference(&url.URL{Path: "/sitemap.xml"}).String(),
		base.ResolveReference(&url.URL{Path: "/sitemap_index.xml"}).String(),
	}
	var out []string
	seen := make(map[string]struct{})

	for _, sm := range candidates {
		body, status, err := fetcher.FetchBytes(ctx, sm)
		if err != nil || status != http.StatusOK || len(body) == 0 {
			if err != nil {
				observability.IncError(observability.ClassifyFetchError(err), "discovery")
			}
			continue
		}
		observability.IncPagesCrawled("discovery")

		var idx sitemapIndex
		if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&idx); err == nil && len(idx.Locations) > 0 {
			for _, loc := range idx.Locations {
				childBody, childStatus, err := fetcher.FetchBytes(ctx, loc.Loc)
				if err != nil || childStatus != http.StatusOK || len(childBody) == 0 {
					if err != nil {
						observability.IncError(observability.ClassifyFetchError(err), "discovery")
					}
					continue
				}
				observability.IncPagesCrawled("discovery")
				var u urlset
				if err := xml.NewDecoder(bytes.NewReader(childBody)).Decode(&u); err == nil {
					for _, link := range u.URLs {
						if acceptSitemapURL(link.Loc) {
							if _, ok := seen[link.Loc]; !ok {
								seen[link.Loc] = struct{}{}
								out = append(out, link.Loc)
							}
						}
					}
				}
			}
			continue
		}

		var u urlset
		if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&u); err == nil {
			for _, link := range u.URLs {
				if acceptSitemapURL(link.Loc) {
					if _, ok := seen[link.Loc]; !ok {
						seen[link.Loc] = struct{}{}
						out = append(out, link.Loc)
					}
				}
			}
		}
	}
	return out
}

func acceptSitemapURL(u string) bool {
	l := strings.ToLower(u)
	return strings.Contains(l, "career") ||
		strings.Contains(l, "job") ||
		strings.Contains(l, "opening") ||
		strings.Contains(l, "position") ||
		strings.Contains(l, "opportun") ||
		strings.Contains(l, "join-us") ||
		strings.Contains(l, "work-with-us") ||
		strings.Contains(l, "early-careers")
}

func resolveLink(base *url.URL, href string) string {
	if strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	return u.String()
}

func hostFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func sameHost(base *url.URL, host string) bool {
	if base == nil || host == "" {
		return false
	}
	baseHost := strings.ToLower(strings.TrimPrefix(base.Hostname(), "www."))
	targetHost := strings.ToLower(strings.TrimPrefix(host, "www."))
	return baseHost == targetHost
}

func isRateLimited(err error) bool {
	if err == nil {
		return false
	}
	var fetchErr *httpx.FetchError
	if errors.As(err, &fetchErr) {
		return fetchErr.Status == http.StatusTooManyRequests
	}
	return false
}
