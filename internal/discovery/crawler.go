package discovery

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/baxromumarov/job-hunter/internal/httpx"
)

// crawler fetches known tech/startup pages and looks for career links.
type crawler struct {
	client *httpx.PoliteClient
}

func newCrawler() *crawler {
	return &crawler{
		client: httpx.NewPoliteClient("job-hunter-bot/1.0"),
	}
}

func (c *crawler) fetchDoc(ctx context.Context, link string) (*goquery.Document, error) {
	req, err := httpx.NewRequest(ctx, link)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return goquery.NewDocumentFromReader(resp.Body)
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

	doc, err := c.fetchDoc(ctx, rawURL)
	if err == nil {
		for _, link := range extractLinks(doc, base) {
			add(link)
		}
		for _, ats := range detectATS(doc, base) {
			add(ats)
		}
	}

	for _, probe := range probePaths(base) {
		if _, ok := seen[probe]; ok {
			continue
		}
		probeDoc, err := c.fetchDoc(ctx, probe)
		if err != nil {
			continue
		}
		add(probe)
		for _, link := range extractLinks(probeDoc, base) {
			add(link)
		}
		for _, ats := range detectATS(probeDoc, base) {
			add(ats)
		}
	}

	for _, link := range parseSitemaps(ctx, c.client, base) {
		add(link)
	}

	return out
}

func extractLinks(doc *goquery.Document, base *url.URL) []string {
	seen := make(map[string]struct{})
	var links []string
	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || href == "" {
			return
		}
		lower := strings.ToLower(href + " " + s.Text())
		if !strings.Contains(lower, "career") &&
			!strings.Contains(lower, "job") &&
			!strings.Contains(lower, "opening") &&
			!strings.Contains(lower, "position") {
			return
		}

		resolved := resolveLink(base, href)
		if resolved == "" {
			return
		}
		if _, ok := seen[resolved]; ok {
			return
		}
		seen[resolved] = struct{}{}
		links = append(links, resolved)
	})
	return links
}

func detectATS(doc *goquery.Document, base *url.URL) []string {
	var ats []string
	hosts := []string{"boards.greenhouse.io", "jobs.lever.co", "jobs.ashbyhq.com", ".workable.com"}

	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || href == "" {
			return
		}
		resolved := resolveLink(base, href)
		if resolved == "" {
			return
		}
		for _, h := range hosts {
			if strings.Contains(resolved, h) {
				ats = append(ats, resolved)
				break
			}
		}
	})
	return ats
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

func parseSitemaps(ctx context.Context, client *httpx.PoliteClient, base *url.URL) []string {
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
		req, err := httpx.NewRequest(ctx, sm)
		if err != nil {
			continue
		}
		resp, err := client.Do(ctx, req)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		var idx sitemapIndex
		if err := xml.NewDecoder(resp.Body).Decode(&idx); err == nil && len(idx.Locations) > 0 {
			resp.Body.Close()
			for _, loc := range idx.Locations {
				reqChild, err := httpx.NewRequest(ctx, loc.Loc)
				if err != nil {
					continue
				}
				child, err := client.Do(ctx, reqChild)
				if err != nil {
					continue
				}
				if child.StatusCode != http.StatusOK {
					child.Body.Close()
					continue
				}
				var u urlset
				if err := xml.NewDecoder(child.Body).Decode(&u); err == nil {
					for _, link := range u.URLs {
						if acceptSitemapURL(link.Loc) {
							if _, ok := seen[link.Loc]; !ok {
								seen[link.Loc] = struct{}{}
								out = append(out, link.Loc)
							}
						}
					}
				}
				child.Body.Close()
			}
			continue
		}
		resp.Body.Close()
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
