package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/baxromumarov/job-hunter/internal/ai"
	"github.com/baxromumarov/job-hunter/internal/api"
	"github.com/baxromumarov/job-hunter/internal/core"
	"github.com/baxromumarov/job-hunter/internal/discovery"
	"github.com/baxromumarov/job-hunter/internal/store"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/jobhunterdb?sslmode=disable"
	}

	dbStore, err := store.NewStore(dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to store: %v", err)
	}
	defer dbStore.Close()

	// Run schema migrations to ensure tables and new columns exist
	workDir, _ := os.Getwd()
	schemaPath := filepath.Join(workDir, "internal", "store", "schema.sql")
	if err := dbStore.RunMigrations(schemaPath); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Initialize AI Client
	aiClient := ai.NewMockClient()

	// Initialize Core Services
	classifier := core.NewClassifierService(aiClient)
	matcher := core.NewMatcherService(aiClient)

	ctx := context.Background()

	// Initialize & Start Discovery Engine
	discoveryEngine := discovery.NewEngine(dbStore, classifier)
	discoveryEngine.StartDiscovery(ctx)

	// Start scraping and retention loop
	ingestion := core.NewIngestionService(dbStore, matcher)
	ingestion.Start(ctx)

	// Initialize API Server
	srv := api.NewServer(dbStore, classifier, matcher)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on port %s", port)
	if err := http.ListenAndServe(":"+port, srv.Router()); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
