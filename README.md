# realestate-leads

Real estate lead generation tool that enriches LLC buyer data from Matrix MLS CSV exports using the Texas Comptroller API and Forewarn.

## Features

✅ **Browser-Automated Forewarn Integration**
- Opens headed Chromium browser for secure login (handles MFA/CAPTCHA)
- Persistent session management with auto-restore
- Smart token refresh (extends session automatically)
- Direct API calls for fast searches

✅ **Intelligent Lead Filtering**
- Automatically filters out deceased leads
- Requires minimum property count (configurable, default 5+)
- Extracts mobile and residential phone numbers
- Records last property purchase date

✅ **Production-Ready Features**
- Real-time progress tracking via web UI
- Server-Sent Events (SSE) for live updates
- Job cancellation mid-processing
- Partial CSV export on cancellation
- Auto-detection of existing login sessions

✅ **Clean Web Interface**
- Localhost web UI (auto-opens on startup)
- Drag-and-drop CSV upload
- Live progress monitoring with scrollable logs
- One-click result download

## Architecture

Go binary that runs a local HTTP server (localhost:8080) with a web UI. Uses `go-rod` to open a headed Chromium browser for Forewarn login, then makes direct API calls to `api.forewarn.com/api/search` using the extracted bearer token.

**Key Components:**
- `cmd/main.go` — Entry point
- `internal/browser/session.go` — Session management & token refresh
- `internal/browser/searcher.go` — Forewarn API client
- `internal/server/server.go` — HTTP server & API endpoints
- `internal/jobs/runner.go` — Job queue & CSV processing
- `web/index.html` — Single-page UI

## Prerequisites

- **Go 1.26+**
- **Chrome/Chromium** (installed automatically by go-rod)
- **Forewarn Account** (for property data searches)

## Installation

### From Source

```bash
git clone https://github.com/mmatz-101/realestate-leads.git
cd realestate-leads
make install  # Install dependencies
make build    # Build binary
```

### Pre-built Binaries

Download from the [releases page](https://github.com/mmatz-101/realestate-leads/releases):
- Linux (amd64, arm64)
- macOS (Intel, Apple Silicon)
- Windows (amd64)

## Quick Start

1. **Set up environment:**
   ```bash
   export TX_COMPTROLLER_API_KEY="your-api-key-here"  # Optional: for preview/full modes
   ```

2. **Start the application:**
   ```bash
   ./build/realestate-leads
   # or
   make run
   ```

3. **Browser opens automatically:**
   - Tab 1: Forewarn login page
   - Tab 2: localhost:8080 (control panel)

4. **Log in to Forewarn** (in the browser)

5. **Upload CSV** in the web UI at localhost:8080

6. **Select processing mode:**
   - **Forewarn Only** (default): Skip comptroller, enrich with Forewarn using existing First Name + Last Name
   - **Preview Mode**: Look up LLC officers from Texas Comptroller only (test data quality)
   - **Full Pipeline**: Stage 1 (Comptroller) → Stage 2 (Forewarn enrichment)

7. **Monitor progress** with real-time updates

8. **Download results** when complete

9. **Continue to Stage 2** (optional): If you ran preview mode, click "Continue to Stage 2" to enrich with Forewarn

## Project Structure

```
realestate-leads/
├── cmd/
│   └── main.go              # Entry point
├── internal/
│   ├── browser/
│   │   ├── session.go       # Session & auth management
│   │   └── searcher.go      # Forewarn API client
│   ├── comptroller/
│   │   ├── client.go        # Texas Comptroller API client
│   │   └── searcher.go      # Comptroller search orchestration
│   ├── jobs/
│   │   └── runner.go        # Multi-stage job queue & CSV processing
│   ├── server/
│   │   └── server.go        # HTTP server & API endpoints
├── web/
│   └── index.html           # Web UI
├── data/
│   ├── uploads/             # Uploaded CSVs (temp)
│   └── output/              # Result CSVs
├── build/                   # Local builds
├── dist/                    # Distribution builds
├── CLAUDE.md                # Development docs
├── PRODUCT_ROADMAP.md       # Future improvements
├── Makefile                 # Build automation
└── README.md                # This file
```

## Build Commands

```bash
# Build for current platform
make build

# Build for all platforms
make build-all

# Run the application
make run

# Clean build artifacts
make clean

# Clean data files
make clean-data

# Run tests
make test

# Show all available commands
make help
```

## Configuration

### CLI Flags

```bash
./build/realestate-leads [options]

Options:
  --port int        Port for web UI (default: 8080)
  --refresh int     Session refresh interval in minutes (default: 30)
  --delay int       Delay between searches in milliseconds (default: 2000)
```

### Example

```bash
# Run on port 3000 with 1 second between searches
./build/realestate-leads --port 3000 --delay 1000
```

## Processing Modes

The application supports three processing modes:

### 1. Forewarn Only (Default)
**Use case:** You already have First Name, Last Name, and Business Address data.

**Input requirements:**
- CSV must contain: `First Name`, `Last Name`, `Business Address`

**What it does:**
- Skips Texas Comptroller lookup
- Goes directly to Forewarn API for property/phone enrichment
- Filters out deceased leads and those with < 5 properties

**Output:** Original columns + Forewarn enrichment data

### 2. Preview Mode (Comptroller Only)
**Use case:** Test data quality, verify LLC officer extraction before paying for Forewarn searches.

**Input requirements:**
- CSV must contain: `Business Name` (LLC names)

**What it does:**
- Stage 1: Looks up LLC officers from Texas Comptroller API
- Extracts `First Name`, `Last Name`, `Business Address` for each LLC
- **Stops here** — download results to verify quality

**Output:** Original columns + `First Name`, `Last Name`, `Business Address`, `Business Name`

**Next step:** Click "Continue to Stage 2" to enrich with Forewarn

### 3. Full Pipeline (Comptroller → Forewarn)
**Use case:** End-to-end processing from LLC names to fully enriched leads.

**Input requirements:**
- CSV must contain: `Business Name` (LLC names)

**What it does:**
- Stage 1: Texas Comptroller API lookup (extracts officers)
- Stage 2: Forewarn API enrichment (property/phone data)
- Fully automated, no manual intervention

**Output:** Original columns + Comptroller data + Forewarn enrichment data

## Input CSV Format

**For Forewarn Only mode:**
- `First Name` (required)
- `Last Name` (required)
- `Business Address` (required, for zip code extraction)

**For Preview/Full modes:**
- `Business Name` (required, LLC names like "SME Homes LLC")

Optional columns are preserved in the output.

## Output CSV Format

Original columns + enrichment data:

| Column | Description |
|--------|-------------|
| `status` | found, skipped, or error |
| `skip_reason` | Why skipped (deceased, low property count, etc.) |
| `full_name` | Full name from Forewarn |
| `age` | Current age |
| `current_address` | Current residential address |
| `mobile_phone` | Mobile phone number |
| `mobile_last_seen` | Last seen date for mobile |
| `residential_phone` | Residential phone number |
| `residential_last_seen` | Last seen date for residential |
| `property_count` | Number of properties owned |
| `last_purchase_date` | Most recent property purchase date |
| `error` | Error message if search failed |

## How It Works

### Authentication Flow

1. Rod opens visible Chromium browser to `app.forewarn.com/login`
2. User logs in manually (handles MFA/CAPTCHA)
3. User clicks "I've logged in" in web UI at localhost:8080
4. App extracts `persist:root` from localStorage → saves to `auth-session.json`
5. Token extracted from `persist:root` → used as `Authorization: bearer <token>`
6. On restart, `persist:root` restored → session persists (no re-login needed)

### Multi-Stage Processing Flow

**Preview Mode (Comptroller Only):**
1. Read `Business Name` column from CSV
2. POST to Texas Comptroller API: `/public-data/v1/public/franchise-tax-list?name={LLC name}`
3. Extract `taxpayerID` from response
4. GET details from `/public-data/v1/public/franchise-tax/{taxpayerID}`
5. Parse `registeredAgentName` → split into `First Name` and `Last Name`
6. Extract `mailingAddress*` → format as `Business Address`
7. Write results to output CSV (Stage 1 complete)
8. **Stop** — user can review and optionally continue to Stage 2

**Full Pipeline (Comptroller → Forewarn):**
1. Run Stage 1 (Comptroller lookup) as above
2. Use Stage 1 output (with First Name, Last Name, Business Address) as input for Stage 2
3. Run Stage 2 (Forewarn enrichment) as below
4. Write final results with both Comptroller and Forewarn data

**Forewarn Only Mode:**
1. Read `First Name`, `Last Name`, `Business Address` from CSV
2. Extract zip from Business Address
3. POST to `https://api.forewarn.com/api/search` with name + zip
4. Check `isDead` → skip if deceased
5. Check property count → skip if < 5 (default)
6. Extract mobile and residential phone numbers
7. Record most recent property purchase date
8. Write results to output CSV

### Token Refresh Strategy

- Every 30 minutes, checks if token expires within 10 minutes
- If yes: calls `/api/authentication/refresh` to extend session
- If no: skips refresh (already fresh from recent searches)
- Forewarn searches automatically refresh token, so active jobs stay fresh

## Development

### Running from Source

```bash
go run cmd/main.go
```

### Project Conventions

- Module path: `github.com/mmatz-101/realestate-leads`
- Go 1.26+
- All internal packages under `internal/` (not importable externally)
- Browser automation via `github.com/go-rod/rod`
- Web UI is a single HTML file served from `web/`
- Auth persisted in `./auth-session.json` (gitignored)
- Browser profile in `./browser-data/` (gitignored)

### Adding Features

See [PRODUCT_ROADMAP.md](PRODUCT_ROADMAP.md) for planned improvements and implementation guides.

## Troubleshooting

### "Failed to start session"
- Ensure Chrome/Chromium is installed
- Check that port 8080 is available
- Try running with `--port 8081` if port conflict

### "Token expired (401)"
- Token has expired (24h max lifetime)
- Restart the app and log in again
- Check the keep-alive logs (should refresh every 30 min)

### "No results" for valid names
- Verify name spelling in Forewarn manually
- Check that zip code is being extracted correctly
- Try different name variations (with/without middle initial)

### Browser doesn't open localhost:8080
- Check firewall settings
- Manually navigate to http://localhost:8080
- Check console for errors

## Security Notes

- `auth-session.json` contains your Forewarn session token
- Keep this file secure and gitignored
- Token expires after 24 hours max
- Browser data stored in `./browser-data/` (gitignored)

## Files to Gitignore

The `.gitignore` is pre-configured for:
```
auth-session.json        # Session tokens
browser-data/            # Browser profile
data/uploads/*.csv       # Uploaded files
data/output/*.csv        # Results
build/                   # Local builds
dist/                    # Distribution builds
franchise_tax_cache.db   # API cache
```

## License

[Add your license here]

## Contributing

[Add contribution guidelines here]

## Support

For issues or questions:
- Open an issue on GitHub
- See [PRODUCT_ROADMAP.md](PRODUCT_ROADMAP.md) for planned features
- Check [CLAUDE.md](CLAUDE.md) for development documentation
