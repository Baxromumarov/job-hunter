package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"
)

type Store struct {
	db *sql.DB
}

func NewStore(connStr string) (*Store, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping db: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) RunMigrations(schemaPath string) error {
	content, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("failed to read schema file: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := s.db.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}

	return nil
}

func clampLimit(limit int, defaultLimit, maxLimit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

type Source struct {
	ID             int        `json:"id"`
	URL            string     `json:"url"`
	Type           string     `json:"type"`
	IsJobSite      bool       `json:"is_job_site"`
	TechRelated    bool       `json:"tech_related"`
	Confidence     float64    `json:"confidence"`
	LastCheckedAt  *time.Time `json:"last_checked_at,omitempty"`
	LastScrapedAt  *time.Time `json:"last_scraped_at,omitempty"`
	DiscoveredAt   *time.Time `json:"discovered_at,omitempty"`
	Classification string     `json:"classification_reason,omitempty"`
}

type Job struct {
	ID           int        `json:"id"`
	SourceID     int        `json:"source_id"`
	SourceURL    string     `json:"source_url"`
	SourceType   string     `json:"source_type"`
	URL          string     `json:"url"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Company      string     `json:"company"`
	Location     string     `json:"location"`
	MatchScore   int        `json:"match_score"`
	MatchSummary string     `json:"match_summary"`
	Applied      bool       `json:"applied"`
	AppliedAt    *time.Time `json:"applied_at,omitempty"`
	PostedAt     *time.Time `json:"posted_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *Store) GetJobs(ctx context.Context, limit, offset int) ([]Job, error) {
	limit = clampLimit(limit, 20, 200)
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT 
    j.id,
    j.source_id,
    s.url as source_url,
    COALESCE(j.source_type, s.type) as source_type,
    j.url,
    j.title,
    j.company,
    j.location,
    j.match_score,
    j.match_summary,
    j.applied,
    j.applied_at,
    j.posted_at,
    j.description,
    j.created_at
FROM jobs j
LEFT JOIN sources s ON s.id = j.source_id
ORDER BY j.applied ASC, j.match_score DESC, COALESCE(j.posted_at, j.created_at) DESC
LIMIT $1 OFFSET $2
`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var (
			j          Job
			appliedAt  sql.NullTime
			postedAt   sql.NullTime
			sourceURL  sql.NullString
			sourceType sql.NullString
			createdAt  time.Time
		)

		if err := rows.Scan(
			&j.ID,
			&j.SourceID,
			&sourceURL,
			&sourceType,
			&j.URL,
			&j.Title,
			&j.Company,
			&j.Location,
			&j.MatchScore,
			&j.MatchSummary,
			&j.Applied,
			&appliedAt,
			&postedAt,
			&j.Description,
			&createdAt,
		); err != nil {
			return nil, err
		}

		j.CreatedAt = createdAt
		if sourceURL.Valid {
			j.SourceURL = sourceURL.String
		}
		if sourceType.Valid {
			j.SourceType = sourceType.String
		}
		if appliedAt.Valid {
			t := appliedAt.Time
			j.AppliedAt = &t
		}
		if postedAt.Valid {
			t := postedAt.Time
			j.PostedAt = &t
		}

		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (s *Store) ListSources(ctx context.Context, limit, offset int) ([]Source, error) {
	limit = clampLimit(limit, 20, 200)
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, url, type, is_job_site, tech_related, confidence, last_checked_at, last_scraped_at, discovered_at, COALESCE(classification_reason, '')
FROM sources
WHERE is_job_site = TRUE
ORDER BY last_scraped_at NULLS FIRST, last_checked_at NULLS FIRST
LIMIT $1 OFFSET $2
`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []Source
	for rows.Next() {
		var (
			src          Source
			lastChecked  sql.NullTime
			lastScraped  sql.NullTime
			discoveredAt sql.NullTime
		)

		if err := rows.Scan(
			&src.ID,
			&src.URL,
			&src.Type,
			&src.IsJobSite,
			&src.TechRelated,
			&src.Confidence,
			&lastChecked,
			&lastScraped,
			&discoveredAt,
			&src.Classification,
		); err != nil {
			return nil, err
		}

		if lastChecked.Valid {
			t := lastChecked.Time
			src.LastCheckedAt = &t
		}
		if lastScraped.Valid {
			t := lastScraped.Time
			src.LastScrapedAt = &t
		}
		if discoveredAt.Valid {
			t := discoveredAt.Time
			src.DiscoveredAt = &t
		}

		sources = append(sources, src)
	}

	return sources, rows.Err()
}

func (s *Store) AddSource(ctx context.Context, url, sourceType string, isJobSite, techRelated bool, confidence float64, reason string) (int, error) {
	var id int
	err := s.db.QueryRowContext(ctx, `
INSERT INTO sources (url, type, is_job_site, tech_related, confidence, classification_reason, last_checked_at, discovered_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
ON CONFLICT (url) DO UPDATE SET
    type = EXCLUDED.type,
    is_job_site = EXCLUDED.is_job_site,
    tech_related = EXCLUDED.tech_related,
    confidence = EXCLUDED.confidence,
    classification_reason = EXCLUDED.classification_reason,
    last_checked_at = NOW()
RETURNING id
`, url, sourceType, isJobSite, techRelated, confidence, reason).Scan(&id)
	return id, err
}

func (s *Store) MarkSourceScraped(ctx context.Context, sourceID int) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE sources
SET last_scraped_at = NOW(), last_checked_at = NOW()
WHERE id = $1
`, sourceID)
	return err
}

func (s *Store) SaveJob(ctx context.Context, job Job) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO jobs (source_id, source_type, url, title, description, company, location, posted_at, match_score, match_summary, applied, applied_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
ON CONFLICT (url) DO UPDATE SET
    source_id = EXCLUDED.source_id,
    source_type = COALESCE(EXCLUDED.source_type, jobs.source_type),
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    company = EXCLUDED.company,
    location = EXCLUDED.location,
    posted_at = COALESCE(jobs.posted_at, EXCLUDED.posted_at),
    match_score = EXCLUDED.match_score,
    match_summary = EXCLUDED.match_summary,
    updated_at = NOW()
`, job.SourceID, job.SourceType, job.URL, job.Title, job.Description, job.Company, job.Location, job.PostedAt, job.MatchScore, job.MatchSummary, job.Applied, job.AppliedAt)
	return err
}

func (s *Store) MarkJobApplied(ctx context.Context, jobID int) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs
SET applied = TRUE, applied_at = NOW(), updated_at = NOW()
WHERE id = $1
`, jobID)
	return err
}

func (s *Store) DeleteOldJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	res, err := s.db.ExecContext(ctx, `
DELETE FROM jobs
WHERE COALESCE(posted_at, created_at) < $1
`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
