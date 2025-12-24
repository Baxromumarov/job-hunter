package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/baxromumarov/job-hunter/internal/content"
	"github.com/baxromumarov/job-hunter/internal/httpx"
	"github.com/baxromumarov/job-hunter/internal/observability"
	"github.com/baxromumarov/job-hunter/internal/store"
	"github.com/baxromumarov/job-hunter/internal/urlutil"
)

// handleListJobs works as a mock for now since we don't have the full DB implementation for Jobs yet
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r, 20)

	jobs, total, activeTotal, err := s.store.GetJobs(r.Context(), limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch jobs: "+err.Error())
		return
	}
	// Return empty list if nil to be JSON friendly
	if jobs == nil {
		jobs = []store.Job{}
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"items":        jobs,
		"limit":        limit,
		"offset":       offset,
		"total":        total,
		"active_total": activeTotal,
	})
}

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r, 20)

	sources, total, err := s.store.ListSources(r.Context(), limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch sources: "+err.Error())
		return
	}
	if sources == nil {
		sources = []store.Source{}
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"items":  sources,
		"limit":  limit,
		"offset": offset,
		"total":  total,
	})
}

type AddSourceRequest struct {
	URL        string `json:"url"`
	SourceType string `json:"source_type"`
}

func parsePagination(r *http.Request, defaultLimit int) (int, int) {
	q := r.URL.Query()
	limit := defaultLimit
	offset := 0

	if v := q.Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			limit = parsed
		}
	}

	if v := q.Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			offset = parsed
		}
	}

	if limit <= 0 {
		limit = defaultLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (s *Server) handleAddSource(w http.ResponseWriter, r *http.Request) {
	var req AddSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.URL == "" {
		respondError(w, http.StatusBadRequest, "URL is required")
		return
	}

	if req.SourceType == "" {
		req.SourceType = "unknown"
	}

	normalized, host, err := urlutil.Normalize(req.URL)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid URL")
		return
	}

	if !urlutil.IsDiscoveryEligible(normalized) {
		_, existed, err := s.store.AddSource(r.Context(), normalized, req.SourceType, urlutil.PageTypeNonJob, false, "", false, false, 0, "ineligible_url", false)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to save source: "+err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"is_job_site":  false,
			"tech_related": false,
			"confidence":   0.0,
			"reason":       "URL not eligible for discovery",
			"existed":      existed,
		})
		return
	}

	if urlutil.IsKnownJobBoardHost(host) {
		canonicalURL, isAlias, err := s.store.ResolveCanonicalSource(r.Context(), normalized, host, urlutil.PageTypeJobList)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to resolve canonical source: "+err.Error())
			return
		}
		if isAlias {
			_, existed, err := s.store.AddSource(r.Context(), normalized, req.SourceType, urlutil.PageTypeJobList, true, canonicalURL, false, false, 0, "alias", false)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "Failed to save source: "+err.Error())
				return
			}
			respondJSON(w, http.StatusOK, map[string]interface{}{
				"is_job_site":  false,
				"tech_related": false,
				"confidence":   0.0,
				"reason":       "Alias of canonical source",
				"existed":      existed,
			})
			return
		}

		_, existed, err := s.store.AddSource(
			r.Context(),
			normalized,
			req.SourceType,
			urlutil.PageTypeJobList,
			false,
			"",
			true,
			true,
			0.9,
			"job_board_allowlist",
			false,
		)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to save source: "+err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"is_job_site":  true,
			"tech_related": true,
			"confidence":   0.9,
			"reason":       "job_board_allowlist",
			"existed":      existed,
		})
		return
	}

	if urlutil.IsATSHost(host) {
		if atsURL, atsHost, err := urlutil.NormalizeATSLink(normalized); err == nil && atsHost != "" {
			normalized = atsURL
			host = atsHost
		}
		pageType := urlutil.DetectPageType(normalized)
		if pageType == urlutil.PageTypeNonJob {
			_, existed, err := s.store.AddSource(r.Context(), normalized, req.SourceType, urlutil.PageTypeNonJob, false, "", false, false, 0, "ats_root", false)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "Failed to save source: "+err.Error())
				return
			}
			respondJSON(w, http.StatusOK, map[string]interface{}{
				"is_job_site":  false,
				"tech_related": false,
				"confidence":   0.0,
				"reason":       "ATS root URL",
				"existed":      existed,
			})
			return
		}

		canonicalURL, isAlias, err := s.store.ResolveCanonicalSource(r.Context(), normalized, host, urlutil.PageTypeJobList)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to resolve canonical source: "+err.Error())
			return
		}
		if isAlias {
			_, existed, err := s.store.AddSource(r.Context(), normalized, req.SourceType, urlutil.PageTypeJobList, true, canonicalURL, false, false, 0, "alias", false)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "Failed to save source: "+err.Error())
				return
			}
			respondJSON(w, http.StatusOK, map[string]interface{}{
				"is_job_site":  false,
				"tech_related": false,
				"confidence":   0.0,
				"reason":       "Alias of canonical source",
				"existed":      existed,
			})
			return
		}

		_, existed, err := s.store.AddSource(
			r.Context(),
			normalized,
			req.SourceType,
			urlutil.PageTypeJobList,
			false,
			"",
			true,
			true,
			0.9,
			"ats_host",
			false,
		)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to save source: "+err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"is_job_site":  true,
			"tech_related": true,
			"confidence":   0.9,
			"reason":       "ats_host",
			"existed":      existed,
		})
		return
	}

	_, existed, err := s.store.AddSource(
		r.Context(),
		normalized,
		req.SourceType,
		urlutil.PageTypeCandidate,
		false,
		"",
		false,
		false,
		0,
		"candidate",
		false,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save candidate: "+err.Error())
		return
	}

	fetcher := httpx.NewCollyFetcher("job-hunter-bot/1.0")
	signals, err := content.Analyze(r.Context(), fetcher, normalized)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch source content: "+err.Error())
		return
	}

	if len(signals.ATSLinks) > 0 {
		observability.IncATSDetected("api")
		addATSSources(r.Context(), s.store, signals.ATSLinks)
		_, _, _ = s.store.AddSource(r.Context(), normalized, req.SourceType, urlutil.PageTypeNonJob, false, "", false, false, 0.9, "ats_link", true)
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"is_job_site":  true,
			"tech_related": true,
			"confidence":   0.9,
			"reason":       "ats_link",
			"existed":      existed,
		})
		return
	}

	decision := content.Classify(signals)
	if decision.PageType == urlutil.PageTypeNonJob {
		_, _, _ = s.store.AddSource(r.Context(), normalized, req.SourceType, urlutil.PageTypeNonJob, false, "", false, false, decision.Confidence, decision.Reason, false)
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"is_job_site":  false,
			"tech_related": false,
			"confidence":   decision.Confidence,
			"reason":       decision.Reason,
			"existed":      existed,
		})
		return
	}

	canonicalURL, isAlias, err := s.store.ResolveCanonicalSource(r.Context(), normalized, host, decision.PageType)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to resolve canonical source: "+err.Error())
		return
	}

	if isAlias {
		_, existed, err := s.store.AddSource(r.Context(), normalized, req.SourceType, decision.PageType, true, canonicalURL, false, false, 0, "alias", false)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to save source: "+err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"is_job_site":  false,
			"tech_related": false,
			"confidence":   0.0,
			"reason":       "Alias of canonical source",
			"existed":      existed,
		})
		return
	}

	_, existed, err = s.store.AddSource(
		r.Context(),
		normalized,
		req.SourceType,
		decision.PageType,
		false,
		"",
		true,
		true,
		decision.Confidence,
		decision.Reason,
		false,
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save source: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"is_job_site":  true,
		"tech_related": true,
		"confidence":   decision.Confidence,
		"reason":       decision.Reason,
		"existed":      existed,
	})
}

func addATSSources(ctx context.Context, st *store.Store, links []string) {
	seen := make(map[string]struct{})
	for _, link := range links {
		normalized, host, err := urlutil.NormalizeATSLink(link)
		if err != nil || host == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}

		pageType := urlutil.PageTypeJobList
		canonicalURL, isAlias, err := st.ResolveCanonicalSource(ctx, normalized, host, pageType)
		if err != nil {
			observability.IncError(observability.ErrorStore, "api")
			continue
		}
		if isAlias {
			_, _, _ = st.AddSource(ctx, normalized, "job_board", pageType, true, canonicalURL, false, false, 0, "alias", false)
			continue
		}

		observability.IncSourcesPromoted("api")
		_, _, _ = st.AddSource(ctx, normalized, "job_board", pageType, false, "", true, true, 0.9, "ats_link", false)
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	sourcesTotal, jobsTotal, activeJobs, err := s.store.GetStatsCounts(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load stats: "+err.Error())
		return
	}

	snapshot := observability.Snapshot()
	if err := s.store.SaveStatsSnapshot(r.Context(), snapshot, sourcesTotal, jobsTotal, activeJobs); err != nil {
		slog.Error("stats snapshot save failed", "error", err)
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"pages_crawled":     snapshot.PagesCrawled,
		"jobs_discovered":   snapshot.JobsDiscovered,
		"jobs_extracted":    snapshot.JobsExtracted,
		"ai_calls":          snapshot.AICalls,
		"errors_total":      snapshot.ErrorsTotal,
		"crawl_avg_seconds": snapshot.CrawlSecondsAvg,
		"urls_discovered":   snapshot.URLsDiscovered,
		"sources_promoted":  snapshot.SourcesPromoted,
		"ats_detected":      snapshot.ATSDetected,
		"sources_zero_jobs": snapshot.SourcesZeroJobs,
		"sources_total":     sourcesTotal,
		"jobs_total":        jobsTotal,
		"active_jobs":       activeJobs,
	})
}

func (s *Server) handleStatsHistory(w http.ResponseWriter, r *http.Request) {
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		respondError(w, http.StatusBadRequest, "Metric is required")
		return
	}

	limit, offset := parsePagination(r, 20)
	items, total, err := s.store.ListStatsHistory(r.Context(), metric, limit, offset)
	if err != nil {
		if errors.Is(err, store.ErrUnknownMetric) {
			respondError(w, http.StatusBadRequest, "Unknown metric")
			return
		}
		respondError(w, http.StatusInternalServerError, "Failed to load stats history: "+err.Error())
		return
	}

	if items == nil {
		items = []store.StatPoint{}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"metric": metric,
		"items":  items,
		"limit":  limit,
		"offset": offset,
		"total":  total,
	})
}

func (s *Server) handleApplyJob(w http.ResponseWriter, r *http.Request) {
	jobIDStr := chi.URLParam(r, "id")
	jobID, err := strconv.Atoi(jobIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid job ID")
		return
	}

	if err := s.store.MarkJobApplied(r.Context(), jobID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to mark job as applied: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]bool{"applied": true})
}

func (s *Server) handleRejectJob(w http.ResponseWriter, r *http.Request) {
	jobIDStr := chi.URLParam(r, "id")
	jobID, err := strconv.Atoi(jobIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid job ID")
		return
	}

	if err := s.store.MarkJobRejected(r.Context(), jobID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to mark job as not a match: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]bool{"rejected": true})
}

func (s *Server) handleCloseJob(w http.ResponseWriter, r *http.Request) {
	jobIDStr := chi.URLParam(r, "id")
	jobID, err := strconv.Atoi(jobIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid job ID")
		return
	}

	if err := s.store.MarkJobClosed(r.Context(), jobID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to mark job as closed: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]bool{"closed": true})
}
