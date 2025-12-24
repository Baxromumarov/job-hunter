CREATE TABLE IF NOT EXISTS sources (
    id SERIAL PRIMARY KEY,
    url TEXT UNIQUE NOT NULL,
    type TEXT NOT NULL DEFAULT 'unknown', -- 'job_board', 'company_page', 'unknown'
    host TEXT,
    normalized_url TEXT,
    is_job_site BOOLEAN DEFAULT FALSE,
    tech_related BOOLEAN DEFAULT FALSE,
    confidence FLOAT DEFAULT 0,
    classification_reason TEXT,
    page_type TEXT DEFAULT 'non_job',
    is_alias BOOLEAN DEFAULT FALSE,
    canonical_url TEXT,
    last_error_type TEXT,
    last_error_message TEXT,
    last_error_at TIMESTAMP WITH TIME ZONE,
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
    rejected BOOLEAN DEFAULT FALSE,
    closed BOOLEAN DEFAULT FALSE,
    closed_at TIMESTAMP WITH TIME ZONE,
    rejected_at TIMESTAMP WITH TIME ZONE,
    applied_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

ALTER TABLE sources ADD COLUMN IF NOT EXISTS last_scraped_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE sources ADD COLUMN IF NOT EXISTS discovered_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();
ALTER TABLE sources ADD COLUMN IF NOT EXISTS classification_reason TEXT;
ALTER TABLE sources ADD COLUMN IF NOT EXISTS host TEXT;
ALTER TABLE sources ADD COLUMN IF NOT EXISTS normalized_url TEXT;
ALTER TABLE sources ADD COLUMN IF NOT EXISTS page_type TEXT DEFAULT 'non_job';
ALTER TABLE sources ADD COLUMN IF NOT EXISTS is_alias BOOLEAN DEFAULT FALSE;
ALTER TABLE sources ADD COLUMN IF NOT EXISTS canonical_url TEXT;
ALTER TABLE sources ADD COLUMN IF NOT EXISTS last_error_type TEXT;
ALTER TABLE sources ADD COLUMN IF NOT EXISTS last_error_message TEXT;
ALTER TABLE sources ADD COLUMN IF NOT EXISTS last_error_at TIMESTAMP WITH TIME ZONE;

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS applied_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS source_type TEXT;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS rejected BOOLEAN DEFAULT FALSE;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS rejected_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS closed BOOLEAN DEFAULT FALSE;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS closed_at TIMESTAMP WITH TIME ZONE;

CREATE INDEX IF NOT EXISTS idx_jobs_match_score ON jobs(match_score);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_applied_at ON jobs(applied_at);
CREATE INDEX IF NOT EXISTS idx_jobs_rejected ON jobs(rejected);
CREATE INDEX IF NOT EXISTS idx_jobs_closed ON jobs(closed);
CREATE INDEX IF NOT EXISTS idx_sources_normalized_url ON sources(normalized_url);
CREATE INDEX IF NOT EXISTS idx_sources_host ON sources(host);
CREATE INDEX IF NOT EXISTS idx_sources_page_type ON sources(page_type);
CREATE INDEX IF NOT EXISTS idx_sources_alias ON sources(is_alias);
