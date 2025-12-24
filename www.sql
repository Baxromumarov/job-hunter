INSERT INTO
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
    updated_at = NOW()