package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/baxromumarov/job-hunter/internal/core"
	"github.com/baxromumarov/job-hunter/internal/store"
)

type Server struct {
	router     *chi.Mux
	store      *store.Store
	classifier *core.ClassifierService
	matcher    *core.MatcherService
}

func NewServer(store *store.Store, classifier *core.ClassifierService, matcher *core.MatcherService) *Server {
	s := &Server{
		router:     chi.NewRouter(),
		store:      store,
		classifier: classifier,
		matcher:    matcher,
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
	}))

	s.router.Get("/health", s.handleHealth)
	s.router.Get("/jobs", s.handleListJobs)
	s.router.Post("/jobs/{id}/apply", s.handleApplyJob)
	s.router.Post("/jobs/{id}/reject", s.handleRejectJob)
	s.router.Post("/jobs/{id}/close", s.handleCloseJob)
	s.router.Get("/sources", s.handleListSources)
	s.router.Post("/sources", s.handleAddSource)

	// Serve static files
	workDir, _ := os.Getwd()
	filesDir := http.Dir(filepath.Join(workDir, "web"))
	FileServer(s.router, "/", filesDir)
}

func FileServer(r chi.Router, path string, root http.FileSystem) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit any URL parameters.")
	}

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", 301).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(path, func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		pathPrefix := strings.TrimSuffix(rctx.RoutePattern(), "/*")
		fs := http.StripPrefix(pathPrefix, http.FileServer(root))
		fs.ServeHTTP(w, r)
	})
}

func (s *Server) Router() http.Handler {
	return s.router
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(response)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}
