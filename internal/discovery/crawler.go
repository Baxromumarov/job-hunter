package discovery

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/baxromumarov/job-hunter/internal/httpx"
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
	var out []string

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

	for _, link := range c.collectLinksFromPage(ctx, rawURL) {
		add(link)
	}

	for _, probe := range probePaths(base) {
		if _, ok := seen[probe]; ok {
			continue
		}
		add(probe)
		for _, link := range c.collectLinksFromPage(ctx, probe) {
			add(link)
		}
	}

	for _, link := range parseSitemaps(ctx, c.fetcher, base) {
		add(link)
	}

	return out
}

func (c *crawler) collectLinksFromPage(ctx context.Context, target string) []string {
	pageBase, err := url.Parse(target)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	var links []string
	_ = c.fetcher.Fetch(ctx, target, func(col *colly.Collector) {
		col.OnHTML("a[href]", func(e *colly.HTMLElement) {
			href := e.Attr("href")
			if href == "" {
				return
			}
			resolved := resolveLink(pageBase, href)
			if resolved == "" {
				return
			}
			if isATSLink(resolved) {
				if _, ok := seen[resolved]; ok {
					return
				}
				seen[resolved] = struct{}{}
				links = append(links, resolved)
				return
			}

			lower := strings.ToLower(href + " " + e.Text)
			if !strings.Contains(lower, "career") &&
				!strings.Contains(lower, "job") &&
				!strings.Contains(lower, "opening") &&
				!strings.Contains(lower, "position") {
				return
			}
			if _, ok := seen[resolved]; ok {
				return
			}
			seen[resolved] = struct{}{}
			links = append(links, resolved)
		})
	})

	return links
}

func probePaths(base *url.URL) []string {
	if base == nil {
		return nil
	}
	paths := []string{"/careers", "/jobs", "/careers/jobs", "/join-us", "/work-with-us"}
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
			continue
		}

		var idx sitemapIndex
		if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&idx); err == nil && len(idx.Locations) > 0 {
			for _, loc := range idx.Locations {
				childBody, childStatus, err := fetcher.FetchBytes(ctx, loc.Loc)
				if err != nil || childStatus != http.StatusOK || len(childBody) == 0 {
					continue
				}
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
		strings.Contains(l, "position")
}

func isATSLink(link string) bool {
	hosts := []string{"boards.greenhouse.io", "jobs.lever.co", "jobs.ashbyhq.com", ".workable.com"}
	for _, h := range hosts {
		if strings.Contains(link, h) {
			return true
		}
	}
	return false
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
