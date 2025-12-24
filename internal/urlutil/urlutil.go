package urlutil

import (
	"net/url"
	"path"
	"sort"
	"strings"
)

const (
	PageTypeCareerRoot = "career_root"
	PageTypeJobList    = "job_list"
	PageTypeJobDetail  = "job_detail"
	PageTypeNonJob     = "non_job"
)

var careerRoots = []string{
	"careers",
	"jobs",
	"join-us",
	"joinus",
	"work-with-us",
	"workwithus",
}

var jobListSegments = []string{
	"jobs",
	"careers",
	"openings",
	"positions",
	"vacancies",
	"job-openings",
	"job-board",
	"jobs-board",
}

var blockedSegments = map[string]struct{}{
	"blog":          {},
	"blogs":         {},
	"events":        {},
	"event":         {},
	"summit":        {},
	"resources":     {},
	"resource":      {},
	"press":         {},
	"news":          {},
	"docs":          {},
	"documentation": {},
	"support":       {},
	"help":          {},
	"legal":         {},
	"privacy":       {},
	"terms":         {},
	"security":      {},
	"engineering":   {},
}

var atsHosts = []string{
	"boards.greenhouse.io",
	"greenhouse.io",
	"jobs.lever.co",
	"lever.co",
	"jobs.ashbyhq.com",
	"ashbyhq.com",
	"workable.com",
}

func Normalize(raw string) (string, string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	u.Fragment = ""
	u.Host = normalizeHost(u.Host)
	u.Path = normalizePath(u.Path)
	u.Path = stripLocalePrefix(u.Path)
	u.RawQuery = normalizeQuery(u.RawQuery)
	return u.String(), u.Hostname(), nil
}

func normalizeHost(host string) string {
	host = strings.ToLower(host)
	host = strings.TrimPrefix(host, "www.")
	return host
}

func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	clean := path.Clean(p)
	if clean == "." {
		return "/"
	}
	if clean != "/" && strings.HasSuffix(clean, "/") {
		clean = strings.TrimSuffix(clean, "/")
	}
	return clean
}

func stripLocalePrefix(p string) string {
	segs := splitPath(p)
	if len(segs) < 2 {
		return p
	}
	if !isLocale(segs[0]) {
		return p
	}
	if isCareerRootSegment(segs[1]) || isJobListSegment(segs[1]) {
		return "/" + strings.Join(segs[1:], "/")
	}
	return p
}

func normalizeQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return ""
	}
	for key := range values {
		lk := strings.ToLower(key)
		if strings.HasPrefix(lk, "utm_") || lk == "gclid" || lk == "fbclid" || lk == "ref" || lk == "source" {
			delete(values, key)
		}
	}
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	normalized := url.Values{}
	for _, k := range keys {
		normalized[k] = values[k]
	}
	return normalized.Encode()
}

func DetectPageType(raw string) string {
	normalized, host, err := Normalize(raw)
	if err != nil {
		return PageTypeNonJob
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return PageTypeNonJob
	}
	segs := splitPath(u.Path)

	if host == "" {
		return PageTypeNonJob
	}

	if IsATSHost(host) {
		if len(segs) == 0 {
			return PageTypeNonJob
		}
		if len(segs) == 1 {
			return PageTypeJobList
		}
		if isJobListSegment(segs[len(segs)-1]) {
			return PageTypeJobList
		}
		return PageTypeJobDetail
	}

	if isBlockedPath(segs) {
		return PageTypeNonJob
	}

	if len(segs) == 0 {
		return PageTypeNonJob
	}

	if isCareerRootPath(segs) {
		if segs[0] == "jobs" {
			return PageTypeJobList
		}
		return PageTypeCareerRoot
	}

	if isJobListPath(segs) {
		return PageTypeJobList
	}

	if isJobDetailPath(segs) {
		return PageTypeJobDetail
	}

	return PageTypeNonJob
}

func IsATSHost(host string) bool {
	h := normalizeHost(host)
	for _, ats := range atsHosts {
		if strings.Contains(h, ats) {
			return true
		}
	}
	return false
}

func CareerRootPriority(raw string) int {
	normalized, host, err := Normalize(raw)
	if err != nil || host == "" {
		return 100
	}
	if IsATSHost(host) {
		return 0
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return 100
	}
	segs := splitPath(u.Path)
	if len(segs) == 0 {
		return 100
	}
	switch segs[0] {
	case "careers":
		return 1
	case "jobs":
		return 2
	case "join-us", "joinus":
		return 4
	case "work-with-us", "workwithus":
		return 5
	default:
		return 10
	}
}

func IsDiscoveryEligible(raw string) bool {
	pt := DetectPageType(raw)
	return pt == PageTypeCareerRoot || pt == PageTypeJobList
}

func splitPath(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}
	return parts
}

func isLocale(seg string) bool {
	if len(seg) == 2 {
		return isAlpha(seg)
	}
	if len(seg) == 5 && seg[2] == '-' {
		return isAlpha(seg[:2]) && isAlpha(seg[3:])
	}
	return false
}

func isAlpha(s string) bool {
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

func isCareerRootSegment(seg string) bool {
	for _, root := range careerRoots {
		if seg == root {
			return true
		}
	}
	return false
}

func isJobListSegment(seg string) bool {
	for _, root := range jobListSegments {
		if seg == root {
			return true
		}
	}
	return false
}

func isCareerRootPath(segs []string) bool {
	return len(segs) == 1 && isCareerRootSegment(segs[0])
}

func isJobListPath(segs []string) bool {
	if len(segs) == 1 && isJobListSegment(segs[0]) {
		return true
	}
	if len(segs) == 2 && segs[0] == "careers" && isJobListSegment(segs[1]) {
		return true
	}
	return false
}

func isJobDetailPath(segs []string) bool {
	for i, seg := range segs {
		if isJobListSegment(seg) || seg == "careers" {
			if i+1 < len(segs) && !isJobListSegment(segs[i+1]) {
				return true
			}
		}
	}
	return false
}

func isBlockedPath(segs []string) bool {
	if len(segs) == 0 {
		return true
	}
	if _, ok := blockedSegments[segs[0]]; ok {
		return true
	}
	if containsJobSegment(segs) {
		return false
	}
	for _, seg := range segs {
		if _, ok := blockedSegments[seg]; ok {
			return true
		}
	}
	return false
}

func containsJobSegment(segs []string) bool {
	for _, seg := range segs {
		if isJobListSegment(seg) || isCareerRootSegment(seg) || seg == "job" {
			return true
		}
	}
	return false
}
