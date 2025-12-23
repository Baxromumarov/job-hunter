CREATE TABLE IF NOT EXISTS sources (
    id SERIAL PRIMARY KEY,
    url TEXT UNIQUE NOT NULL,
    type TEXT NOT NULL DEFAULT 'unknown', -- 'job_board', 'company_page', 'unknown'
    is_job_site BOOLEAN DEFAULT FALSE,
    tech_related BOOLEAN DEFAULT FALSE,
    confidence FLOAT DEFAULT 0,
    classification_reason TEXT,
    last_checked_at TIMESTAMP WITH TIME ZONE,
    last_scraped_at TIMESTAMP WITH TIME ZONE,
    discovered_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS jobs (
    id SERIAL PRIMARY KEY,
    source_id INT REFERENCES sources(id),
    url TEXT UNIQUE NOT NULL,
    source_type TEXT,
    title TEXT NOT NULL,
    description TEXT,
    company TEXT,
    location TEXT,
    salary_range TEXT,
    posted_at TIMESTAMP WITH TIME ZONE,
    match_score INT DEFAULT 0,
    match_summary TEXT, -- JSON or text summary
    applied BOOLEAN DEFAULT FALSE,
    applied_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

ALTER TABLE sources ADD COLUMN IF NOT EXISTS last_scraped_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE sources ADD COLUMN IF NOT EXISTS discovered_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();
ALTER TABLE sources ADD COLUMN IF NOT EXISTS classification_reason TEXT;

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS applied_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS source_type TEXT;

CREATE INDEX IF NOT EXISTS idx_jobs_match_score ON jobs(match_score);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_applied_at ON jobs(applied_at);
