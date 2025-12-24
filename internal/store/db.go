package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"

	"github.com/baxromumarov/job-hunter/internal/urlutil"
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

	return min(limit, maxLimit)
}

func normalizePagination(limit, offset int) (int, int) {
	limit = clampLimit(limit, 20, 200)
	if offset < 0 {
		offset = 0
	}

	return limit, offset
}

func scanNullTime(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}

	t := nt.Time
	return &t
}

type Source struct {
	ID             int        `json:"id"`
	URL            string     `json:"url"`
	NormalizedURL  string     `json:"normalized_url,omitempty"`
	Host           string     `json:"host,omitempty"`
	Type           string     `json:"type"`
	PageType       string     `json:"page_type,omitempty"`
	IsAlias        bool       `json:"is_alias,omitempty"`
	CanonicalURL   string     `json:"canonical_url,omitempty"`
	IsJobSite      bool       `json:"is_job_site"`
	TechRelated    bool       `json:"tech_related"`
	Confidence     float64    `json:"confidence"`
	LastCheckedAt  *time.Time `json:"last_checked_at,omitempty"`
	LastScrapedAt  *time.Time `json:"last_scraped_at,omitempty"`
	DiscoveredAt   *time.Time `json:"discovered_at,omitempty"`
	Classification string     `json:"classification_reason,omitempty"`
	LastErrorType  string     `json:"last_error_type,omitempty"`
	LastErrorMsg   string     `json:"last_error_message,omitempty"`
	LastErrorAt    *time.Time `json:"last_error_at,omitempty"`
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
	Rejected     bool       `json:"rejected"`
	RejectedAt   *time.Time `json:"rejected_at,omitempty"`
	Closed       bool       `json:"closed"`
	ClosedAt     *time.Time `json:"closed_at,omitempty"`
	PostedAt     *time.Time `json:"posted_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *Store) GetJobs(ctx context.Context, limit, offset int) ([]Job, int, int, error) {
	limit, offset = normalizePagination(limit, offset)

	var total int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT 
			COUNT(*) 
		FROM 
			jobs`,
	).Scan(
		&total,
	); err != nil {
		return nil, 0, 0, err
	}

	var activeTotal int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM jobs WHERE rejected = FALSE AND closed = FALSE`,
	).Scan(
		&activeTotal,
	); err != nil {
		return nil, 0, 0, err
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT 
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
    		j.rejected,
    		j.rejected_at,
    		j.closed,
    		j.closed_at,
    		j.posted_at,
    		j.description,
    		j.created_at
		FROM 
			jobs j
		LEFT JOIN 
			sources s ON s.id = j.source_id
		ORDER BY 
			j.applied ASC, 
			j.match_score DESC, 
			COALESCE(j.posted_at, j.created_at) DESC
		LIMIT 
			$1 
		OFFSET 
			$2`,
		limit,
		offset,
	)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var (
			j          Job
			appliedAt  sql.NullTime
			rejectedAt sql.NullTime
			closedAt   sql.NullTime
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
			&j.Rejected,
			&rejectedAt,
			&j.Closed,
			&closedAt,
			&postedAt,
			&j.Description,
			&createdAt,
		); err != nil {
			return nil, 0, 0, err
		}

		if sourceURL.Valid {
			j.SourceURL = sourceURL.String
		}

		if sourceType.Valid {
			j.SourceType = sourceType.String
		}

		j.AppliedAt = scanNullTime(appliedAt)
		j.RejectedAt = scanNullTime(rejectedAt)
		j.ClosedAt = scanNullTime(closedAt)
		j.PostedAt = scanNullTime(postedAt)
		j.CreatedAt = createdAt

		jobs = append(jobs, j)
	}
	return jobs, total, activeTotal, rows.Err()
}

func (s *Store) ListSources(ctx context.Context, limit, offset int) ([]Source, int, error) {
	limit, offset = normalizePagination(limit, offset)

	var total int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT 
			COUNT(*) 
		FROM 
			sources 
		WHERE 
			is_job_site = TRUE
			AND is_alias = FALSE
			AND page_type IN ('career_root', 'job_list')`,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT 
			id, 
			url, 
			COALESCE(normalized_url, ''),
			COALESCE(host, ''),
			type, 
			COALESCE(page_type, ''),
			is_alias,
			COALESCE(canonical_url, ''),
			is_job_site, 
			tech_related, 
			confidence, 
			last_checked_at, 
			last_scraped_at, 
			discovered_at, 
			COALESCE(classification_reason, ''),
			COALESCE(last_error_type, ''),
			COALESCE(last_error_message, ''),
			last_error_at
		FROM 
			sources
		WHERE 
			is_job_site = TRUE
			AND is_alias = FALSE
			AND page_type IN ('career_root', 'job_list')
		ORDER BY 
			last_scraped_at NULLS FIRST, 
			last_checked_at NULLS FIRST
		LIMIT 
			$1 
		OFFSET 
			$2`,
		limit,
		offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var sources []Source
	for rows.Next() {
		var (
			src          Source
			lastChecked  sql.NullTime
			lastScraped  sql.NullTime
			discoveredAt sql.NullTime
			lastErrorAt  sql.NullTime
		)

		if err := rows.Scan(
			&src.ID,
			&src.URL,
			&src.NormalizedURL,
			&src.Host,
			&src.Type,
			&src.PageType,
			&src.IsAlias,
			&src.CanonicalURL,
			&src.IsJobSite,
			&src.TechRelated,
			&src.Confidence,
			&lastChecked,
			&lastScraped,
			&discoveredAt,
			&src.Classification,
			&src.LastErrorType,
			&src.LastErrorMsg,
			&lastErrorAt,
		); err != nil {
			return nil, 0, err
		}

		src.LastCheckedAt = scanNullTime(lastChecked)
		src.LastScrapedAt = scanNullTime(lastScraped)
		src.DiscoveredAt = scanNullTime(discoveredAt)
		src.LastErrorAt = scanNullTime(lastErrorAt)

		sources = append(sources, src)
	}

	return sources, total, rows.Err()
}

func (s *Store) AddSource(
	ctx context.Context,
	rawURL,
	sourceType,
	pageType string,
	isAlias bool,
	canonicalURL string,
	isJobSite,
	techRelated bool,
	confidence float64,
	reason string,
) (
	id int,
	existed bool,
	err error,
) {
	normalized, host, err := urlutil.Normalize(rawURL)
	if err != nil {
		normalized = rawURL
	}
	if pageType == "" {
		pageType = urlutil.PageTypeNonJob
	}

	var existingID sql.NullInt64
	if err = s.db.QueryRowContext(
		ctx,
		`SELECT 
			id 
		FROM 
			sources 
		WHERE 
			normalized_url = $1
			OR url = $2`,
		normalized,
		rawURL,
	).Scan(
		&existingID,
	); err != nil && err != sql.ErrNoRows {
		return 0, false, err
	}

	if existingID.Valid {
		existed = true
		id = int(existingID.Int64)
		_, err = s.db.ExecContext(
			ctx,
			`UPDATE
				sources
			SET
				url = $1,
				normalized_url = $2,
				host = $3,
				type = $4,
				page_type = $5,
				is_alias = $6,
				canonical_url = NULLIF($7, ''),
				is_job_site = $8,
				tech_related = $9,
				confidence = $10,
				classification_reason = $11,
				last_checked_at = NOW()
			WHERE
				id = $12`,
			rawURL,
			normalized,
			host,
			sourceType,
			pageType,
			isAlias,
			canonicalURL,
			isJobSite,
			techRelated,
			confidence,
			reason,
			id,
		)
		return id, existed, err
	}

	err = s.db.QueryRowContext(
		ctx,
		`INSERT INTO
    		sources (
				url,
				normalized_url,
				host,
        		type,
				page_type,
				is_alias,
				canonical_url,
        		is_job_site,
        		tech_related,
        		confidence,
        		classification_reason,
        		last_checked_at,
        		discovered_at
    		)
		VALUES
    		(
				$1,
				$2,
				$3,
        		$4,
				$5,
				$6,
				NULLIF($7, ''),
        		$8,
        		$9,
        		$10,
        		$11,
        		NOW(),
        		NOW()
    		)
		RETURNING id;`,
		rawURL,
		normalized,
		host,
		sourceType,
		pageType,
		isAlias,
		canonicalURL,
		isJobSite,
		techRelated,
		confidence,
		reason,
	).Scan(&id)
	return id, existed, err
}

func (s *Store) MarkSourceScraped(ctx context.Context, sourceID int) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE 
			sources
		SET 
			last_scraped_at = NOW(), 
			last_checked_at = NOW()
		WHERE 
			id = $1`,
		sourceID,
	)

	return err
}

func (s *Store) FindSourceByURL(ctx context.Context, rawURL string) (*Source, error) {
	normalized, _, err := urlutil.Normalize(rawURL)
	if err != nil {
		normalized = rawURL
	}
	row := s.db.QueryRowContext(
		ctx,
		`SELECT 
			id,
			url,
			COALESCE(normalized_url, ''),
			COALESCE(host, ''),
			COALESCE(page_type, ''),
			is_alias,
			COALESCE(canonical_url, ''),
			is_job_site
		FROM
			sources
		WHERE
			normalized_url = $1
			OR url = $1
			OR url = $2
		LIMIT 1`,
		normalized,
		rawURL,
	)

	var src Source
	if err := row.Scan(
		&src.ID,
		&src.URL,
		&src.NormalizedURL,
		&src.Host,
		&src.PageType,
		&src.IsAlias,
		&src.CanonicalURL,
		&src.IsJobSite,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &src, nil
}

func (s *Store) GetCanonicalSourceByHost(ctx context.Context, host string) (*Source, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT 
			id,
			url,
			COALESCE(normalized_url, ''),
			COALESCE(host, ''),
			COALESCE(page_type, ''),
			is_alias,
			COALESCE(canonical_url, '')
		FROM
			sources
		WHERE
			host = $1
			AND is_alias = FALSE
			AND is_job_site = TRUE
			AND page_type IN ('career_root', 'job_list')
		ORDER BY
			discovered_at ASC
		LIMIT 1`,
		host,
	)

	var src Source
	if err := row.Scan(
		&src.ID,
		&src.URL,
		&src.NormalizedURL,
		&src.Host,
		&src.PageType,
		&src.IsAlias,
		&src.CanonicalURL,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &src, nil
}

func (s *Store) MarkSourceAlias(ctx context.Context, sourceID int, canonicalURL string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE
			sources
		SET
			is_alias = TRUE,
			canonical_url = NULLIF($1, ''),
			last_checked_at = NOW()
		WHERE
			id = $2`,
		canonicalURL,
		sourceID,
	)
	return err
}

func (s *Store) ResolveCanonicalSource(ctx context.Context, normalizedURL, host, pageType string) (string, bool, error) {
	if pageType != urlutil.PageTypeCareerRoot && pageType != urlutil.PageTypeJobList {
		return normalizedURL, false, nil
	}
	if host == "" {
		return normalizedURL, false, nil
	}

	existing, err := s.GetCanonicalSourceByHost(ctx, host)
	if err != nil {
		return normalizedURL, false, err
	}
	if existing == nil {
		return normalizedURL, false, nil
	}

	existingPriority := urlutil.CareerRootPriority(existing.URL)
	newPriority := urlutil.CareerRootPriority(normalizedURL)
	if existingPriority <= newPriority {
		return existing.URL, true, nil
	}

	if err := s.MarkSourceAlias(ctx, existing.ID, normalizedURL); err != nil {
		return normalizedURL, false, err
	}
	return normalizedURL, false, nil
}

func (s *Store) MarkSourceError(ctx context.Context, sourceID int, errType, message string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE
			sources
		SET
			last_error_type = $1,
			last_error_message = $2,
			last_error_at = NOW(),
			last_checked_at = NOW()
		WHERE
			id = $3`,
		errType,
		truncateString(message, 800),
		sourceID,
	)
	return err
}

func (s *Store) MarkSourceErrorByURL(ctx context.Context, rawURL, errType, message string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE
			sources
		SET
			last_error_type = $1,
			last_error_message = $2,
			last_error_at = NOW(),
			last_checked_at = NOW()
		WHERE
			normalized_url = $3
			OR url = $3`,
		errType,
		truncateString(message, 800),
		rawURL,
	)
	return err
}

func (s *Store) ClearSourceError(ctx context.Context, sourceID int) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE
			sources
		SET
			last_error_type = NULL,
			last_error_message = NULL,
			last_error_at = NULL
		WHERE
			id = $1`,
		sourceID,
	)
	return err
}

func (s *Store) SaveJob(ctx context.Context, job Job) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO
		    jobs (
		        source_id,
		        source_type,
		        url,
		        title,
		        description,
		        company,
		        location,
		        posted_at,
		        match_score,
		        match_summary,
		        applied,
		        applied_at,
		        rejected,
		        rejected_at,
		        closed,
		        closed_at,
		        created_at
		    )
		VALUES
		    (
		        $1,
		        $2,
		        $3,
		        $4,
		        $5,
		        $6,
		        $7,
		        $8,
		        $9,
		        $10,
		        $11,
		        $12,
		        $13,
		        $14,
		        $15,
		        $16,
		        NOW()
		    ) ON CONFLICT (url) DO
		UPDATE
		SET
		    source_id = EXCLUDED.source_id,
		    source_type = COALESCE(EXCLUDED.source_type, jobs.source_type),
		    title = EXCLUDED.title,
		    description = EXCLUDED.description,
		    company = EXCLUDED.company,
		    location = EXCLUDED.location,
		    posted_at = COALESCE(jobs.posted_at, EXCLUDED.posted_at),
		    match_score = EXCLUDED.match_score,
		    match_summary = EXCLUDED.match_summary,
		    updated_at = NOW()`,
		job.SourceID,
		job.SourceType,
		job.URL,
		job.Title,
		job.Description,
		job.Company,
		job.Location,
		job.PostedAt,
		job.MatchScore,
		job.MatchSummary,
		job.Applied,
		job.AppliedAt,
		job.Rejected,
		job.RejectedAt,
		job.Closed,
		job.ClosedAt,
	)
	return err
}

func (s *Store) MarkJobApplied(ctx context.Context, jobID int) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE 
			jobs
		SET 
			applied = TRUE, 
			applied_at = NOW(),
			updated_at = NOW()
		WHERE 
			id = $1`,
		jobID,
	)
	return err
}

func (s *Store) MarkJobRejected(ctx context.Context, jobID int) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE 
			jobs
		SET 
			rejected = TRUE, 
			rejected_at = NOW(), 
			updated_at = NOW()
		WHERE 
			id = $1`,
		jobID,
	)
	return err
}

func (s *Store) MarkJobClosed(ctx context.Context, jobID int) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE 
			jobs
		SET 
			closed = TRUE, 
			closed_at = NOW(), 
			updated_at = NOW()
		WHERE 
			id = $1`,
		jobID,
	)
	return err
}

func (s *Store) DeleteOldJobs(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)

	res, err := s.db.ExecContext(
		ctx,
		`DELETE FROM 
			jobs
		WHERE 
			COALESCE(posted_at, created_at) < $1`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) GetStatsCounts(ctx context.Context) (sourcesTotal, jobsTotal, activeJobs int, err error) {
	if err = s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sources WHERE is_job_site = TRUE AND is_alias = FALSE AND page_type IN ('career_root', 'job_list')`,
	).Scan(&sourcesTotal); err != nil {
		return 0, 0, 0, err
	}

	if err = s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM jobs`,
	).Scan(&jobsTotal); err != nil {
		return 0, 0, 0, err
	}

	if err = s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM jobs WHERE rejected = FALSE AND closed = FALSE`,
	).Scan(&activeJobs); err != nil {
		return 0, 0, 0, err
	}

	return sourcesTotal, jobsTotal, activeJobs, nil
}

func truncateString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
