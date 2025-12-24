package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/baxromumarov/job-hunter/internal/store"
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

	// Trigger classification
	// In a real app this might be async or queued
	classification, err := s.classifier.Classify(r.Context(), req.URL, "Mock Title", "Mock Meta", "Mock Text Sample from "+req.URL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Classification failed: "+err.Error())
		return
	}

	// Save source to DB
	_, existed, err := s.store.AddSource(r.Context(), req.URL, req.SourceType, classification.IsJobSite, classification.TechRelated, classification.Confidence, classification.Reason)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to save source: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"is_job_site":  classification.IsJobSite,
		"tech_related": classification.TechRelated,
		"confidence":   classification.Confidence,
		"reason":       classification.Reason,
		"existed":      existed,
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
