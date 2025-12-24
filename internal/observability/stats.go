package observability

import (
	"sync"
	"sync/atomic"
)

type StatsSnapshot struct {
	PagesCrawled      uint64            `json:"pages_crawled"`
	JobsDiscovered    uint64            `json:"jobs_discovered"`
	JobsExtracted     uint64            `json:"jobs_extracted"`
	AICalls           uint64            `json:"ai_calls"`
	ErrorsTotal       uint64            `json:"errors_total"`
	CrawlSecondsAvg   float64           `json:"crawl_seconds_avg"`
	URLsDiscovered    uint64            `json:"urls_discovered"`
	SourcesPromoted   uint64            `json:"sources_promoted"`
	ATSDetected       uint64            `json:"ats_detected"`
	SourcesZeroJobs   uint64            `json:"sources_zero_jobs"`
	SourceDecisions   map[string]uint64 `json:"source_decisions,omitempty"`
	ErrorsByType      map[string]uint64 `json:"errors_by_type,omitempty"`
	ErrorsByComponent map[string]uint64 `json:"errors_by_component,omitempty"`
}

var (
	pagesCrawled    uint64
	jobsDiscovered  uint64
	jobsExtracted   uint64
	aiCalls         uint64
	errorsTotal     uint64
	urlsDiscovered  uint64
	sourcesPromoted uint64
	atsDetected     uint64
	sourcesZeroJobs uint64

	crawlCount uint64
	crawlNanos uint64

	statsMu           sync.Mutex
	sourceDecisions   = map[string]uint64{}
	errorsByType      = map[string]uint64{}
	errorsByComponent = map[string]uint64{}
)

func RegisterMetrics() {
	// no-op (left for compatibility)
}

func IncPagesCrawled(_ string) {
	atomic.AddUint64(&pagesCrawled, 1)
}

func IncJobsDiscovered(_ string) {
	atomic.AddUint64(&jobsDiscovered, 1)
}

func IncJobsExtracted(_ string) {
	atomic.AddUint64(&jobsExtracted, 1)
}

func IncURLsDiscovered(_ string) {
	atomic.AddUint64(&urlsDiscovered, 1)
}

func IncSourcesPromoted(_ string) {
	atomic.AddUint64(&sourcesPromoted, 1)
}

func IncATSDetected(_ string) {
	atomic.AddUint64(&atsDetected, 1)
}

func IncSourcesZeroJobs(_ string) {
	atomic.AddUint64(&sourcesZeroJobs, 1)
}

func IncAICall(_ string) {
	atomic.AddUint64(&aiCalls, 1)
}

func IncSourceDecision(result string) {
	if result == "" {
		result = "unknown"
	}
	statsMu.Lock()
	sourceDecisions[result]++
	statsMu.Unlock()
}

func ObserveCrawlDuration(_ string, seconds float64) {
	if seconds <= 0 {
		return
	}
	atomic.AddUint64(&crawlCount, 1)
	atomic.AddUint64(&crawlNanos, uint64(seconds*1e9))
}

func IncError(errType, component string) {
	if errType == "" {
		errType = "unknown"
	}
	if component == "" {
		component = "unknown"
	}
	atomic.AddUint64(&errorsTotal, 1)
	statsMu.Lock()
	errorsByType[errType]++
	errorsByComponent[component]++
	statsMu.Unlock()
}

func Snapshot() StatsSnapshot {
	statsMu.Lock()
	sourceCopy := copyMap(sourceDecisions)
	errorsTypeCopy := copyMap(errorsByType)
	errorsComponentCopy := copyMap(errorsByComponent)
	statsMu.Unlock()

	count := atomic.LoadUint64(&crawlCount)
	avg := 0.0
	if count > 0 {
		avg = float64(atomic.LoadUint64(&crawlNanos)) / float64(count) / 1e9
	}

	return StatsSnapshot{
		PagesCrawled:      atomic.LoadUint64(&pagesCrawled),
		JobsDiscovered:    atomic.LoadUint64(&jobsDiscovered),
		JobsExtracted:     atomic.LoadUint64(&jobsExtracted),
		AICalls:           atomic.LoadUint64(&aiCalls),
		ErrorsTotal:       atomic.LoadUint64(&errorsTotal),
		CrawlSecondsAvg:   avg,
		URLsDiscovered:    atomic.LoadUint64(&urlsDiscovered),
		SourcesPromoted:   atomic.LoadUint64(&sourcesPromoted),
		ATSDetected:       atomic.LoadUint64(&atsDetected),
		SourcesZeroJobs:   atomic.LoadUint64(&sourcesZeroJobs),
		SourceDecisions:   sourceCopy,
		ErrorsByType:      errorsTypeCopy,
		ErrorsByComponent: errorsComponentCopy,
	}
}

func copyMap(src map[string]uint64) map[string]uint64 {
	if len(src) == 0 {
		return map[string]uint64{}
	}
	out := make(map[string]uint64, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
