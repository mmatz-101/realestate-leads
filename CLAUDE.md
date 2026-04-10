# realestate-leads

Real estate lead generation tool that enriches LLC buyer data from Matrix MLS CSV exports using the Texas Comptroller API and Forewarn.

## Architecture

Go binary that runs a local HTTP server (localhost:8080) with a web UI. Uses `go-rod` to open a headed Chromium browser for Forewarn login, then makes direct API calls to `api.forewarn.com/api/search` using the extracted bearer token.

### Key Components

- **`cmd/main.go`** — Entry point. Starts session manager, HTTP server, opens browser for login.
- **`internal/browser/session.go`** — Manages Chromium browser via rod. Handles login flow, saves/restores `persist:root` from Forewarn's localStorage, extracts bearer token (`sessionId`) for API auth. Keep-alive loop refreshes session every 30 min.
- **`internal/browser/searcher.go`** — Pure HTTP client. POSTs to `https://api.forewarn.com/api/search` with `{"firstName","lastName","zip"}`. Parses JSON response for phone numbers, property count, deceased status. No browser automation needed for searches.
- **`internal/server/server.go`** — HTTP server serving the web UI and API endpoints (upload CSV, start job, SSE progress, download results).
- **`internal/jobs/runner.go`** — Job queue. Parses CSV, orchestrates searches, tracks progress, writes result CSV. Broadcasts progress via SSE to the web UI.
- **`internal/comptroller/`** — Texas Comptroller API client (existing, for LLC officer lookup).
- **`internal/csv/`** — CSV reader/writer with LLC detection (existing).
- **`web/index.html`** — Single-page UI for login confirmation, CSV upload, progress monitoring, result download.

### Auth Flow

1. Rod opens a visible Chromium browser to `app.forewarn.com/login`
2. User logs in manually (handles MFA/CAPTCHA)
3. User clicks "I've logged in" in the web UI at localhost:8080
4. `saveAuth()` finds the Forewarn tab, grabs `persist:root` from localStorage, saves to `./auth-session.json`
5. `GetToken()` parses `persist:root` → `authentication` (JSON string) → `session.sessionId` → UUID used as `Authorization: bearer <token>`
6. On restart, `restoreAuth()` injects saved `persist:root` back into localStorage so the browser session persists

### Search Flow

For each CSV row with a First Name + Last Name:
1. Extract zip from `Business Address` field
2. POST to Forewarn API with name + zip
3. Check `isDead` — skip if deceased
4. Check `property` array length — skip if < 5
5. Extract first Mobile and first Residential phone number
6. Write results to output CSV

### CSV Format

Input columns: `Original Business Name`, `First Name`, `Last Name`, `Business Address`, `Business Name`

Output adds: `status`, `skip_reason`, `full_name`, `age`, `current_address`, `mobile_phone`, `mobile_last_seen`, `residential_phone`, `residential_last_seen`, `property_count`, `error`

## Build & Run

```bash
go mod tidy
go run cmd/main.go
# or
go build -o leads cmd/main.go
./leads
```

The login URL and search URL are hardcoded in `cmd/main.go`.

## Project Conventions

- Module path: `github.com/mmatz-101/realestate-leads`
- Go 1.26+
- All internal packages under `internal/` (not importable externally)
- Browser automation via `github.com/go-rod/rod`
- Web UI is a single HTML file served from `web/`
- Auth persisted in `./auth-session.json` (gitignored)
- Browser profile in `./browser-data/` (gitignored)

## Files to gitignore

```
auth-session.json
browser-data/
*.csv
```

## Current Status / TODOs

- [ ] Verify `saveAuth` correctly captures `persist:root` via rod's `Eval` (had issues with `gson.JSON` value extraction — `fmt.Sprintf("%v", result.Value)` is the current workaround)
- [ ] Test full end-to-end: login → save auth → restart → restore → upload CSV → API searches → output CSV
- [ ] Add rate limiting / backoff for Forewarn API calls
- [ ] Handle token expiry mid-job (detect 401, prompt re-login)
- [ ] Consider Tauri wrapper for distribution later
