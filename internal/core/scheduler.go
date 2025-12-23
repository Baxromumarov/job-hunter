package core

import (
	"context"
	"log"
	"time"

	"github.com/baxromumarov/job-hunter/internal/store"
)

type SchedulerService struct {
	store *store.Store
}

func NewSchedulerService(store *store.Store) *SchedulerService {
	return &SchedulerService{store: store}
}

func (s *SchedulerService) Start(ctx context.Context) {
	go s.runRetentionPolicy(ctx)
}

// runRetentionPolicy deletes jobs older than 30 days
func (s *SchedulerService) runRetentionPolicy(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour) // Run once a day
	defer ticker.Stop()

	// Run immediately on startup
	s.cleanup()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

func (s *SchedulerService) cleanup() {
	count, err := s.store.DeleteOldJobs(context.Background(), 30*24*time.Hour)
	if err != nil {
		log.Printf("Retention Policy: Failed to cleanup old jobs: %v", err)
	} else {
		log.Printf("Retention Policy: Deleted %d old jobs", count)
	}
}
