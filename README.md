# Job Hunter

Job Hunter is a Go service that discovers job sources, scrapes job boards and ATS-backed career pages, and ranks new postings against a Go/backend profile. It stores results in Postgres, exposes a JSON API, and serves a lightweight web UI from `web/`.

## Features
- Auto-discovery of sources via seeds, crawling, and search
- Scrapers for RemoteOK, We Work Remotely, Greenhouse, Lever, Ashby, plus a generic fallback
- AI-assisted source classification and job matching (Gemini or mock)
- Source management, job actions, and system stats endpoints
- Automatic schema migrations and stale-job cleanup

## Quickstart
1. Start Postgres and create a database.
2. Set environment variables as needed:
   - `DATABASE_URL` (default: `postgres://postgres:postgres@localhost:5432/jobhunterdb?sslmode=disable`)
   - `PORT` (default: `8080`)
   - `AI_PROVIDER` (`gemini` or `mock`)
   - `GEMINI_API_KEY` (required for Gemini)
   - `JOB_MIN_MATCH_SCORE` (0-100, default: 60)
3. Run the server:
   - `go run ./cmd/server`
4. Open `http://localhost:8080` to view the UI.

## API (selected)
- `GET /health`
- `GET /jobs`
- `POST /jobs/{id}/apply`
- `POST /jobs/{id}/reject`
- `POST /jobs/{id}/close`
- `GET /sources`
- `POST /sources`
- `GET /stats`
- `GET /stats/history?metric=...`
