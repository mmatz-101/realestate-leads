# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This is a Go CLI application that enriches real estate lead data by querying the Texas Comptroller's public franchise tax API. The application reads business names from a CSV file, looks up their franchise tax records, and outputs enriched data including registered agent names and mailing addresses.

## Architecture

**Modular application** with clean separation of concerns:
- `main.go` - CLI and orchestration
- `api.go` - Texas Comptroller API client
- `cache.go` - SQLite caching layer
- `retry.go` - Retry logic and adaptive rate limiting
- `utils.go` - Utility functions

**External dependencies**:
- `modernc.org/sqlite` - SQLite database driver

**Core workflow**:
1. Initialize SQLite cache (`franchise_tax_cache.db`)
2. Read input CSV with a "Business Name" column
3. For each business name:
   - Check cache first (instant lookup if cached)
   - If not cached: Search franchise tax records via `/public-data/v1/public/franchise-tax-list`
   - Get detailed information via `/public-data/v1/public/franchise-tax/{taxpayerID}`
   - Store result in cache (both successful and failed lookups)
4. Extract registered agent name, split into first/last, format mailing address
5. Write to timestamped output CSV with enriched data

**Key components**:
- `Cache.Get()/Put()` - SQLite cache for storing lookups (cache.go)
- `APIClient.SearchFranchiseTax()` - Searches by business name, returns taxpayer ID (api.go)
- `APIClient.GetFranchiseTaxDetail()` - Fetches full record including agent and address (api.go)
- `WithRetry()` - Retry logic with exponential backoff for 429 errors (retry.go)
- `AdaptiveDelay` - Dynamic rate limiting (100ms → 200ms after 429s) (retry.go)
- `splitName()` - Parses registered agent name into first/last (utils.go)
- `formatAddress()` - Combines address components into single string (utils.go)
- Rate limiting: 100ms base delay, 200ms in adaptive mode (see `requestDelay` constant)

**API integration**:
- Base URL: `https://api.comptroller.texas.gov`
- Requires `TX_COMPTROLLER_API_KEY` environment variable
- Uses `x-api-key` header for authentication
- 30-second HTTP timeout

**Data structures**:
- `searchResponse` - API search results with taxpayer IDs
- `detailResponse` - Full franchise tax record with address/agent info

## Development Commands

**Build and run**:
```bash
go build -o realestate-leads
./realestate-leads --file input.csv
```

**Run directly**:
```bash
go run main.go --file input.csv
```

**Format code**:
```bash
go fmt main.go
```

**Required environment**:
```bash
export TX_COMPTROLLER_API_KEY="your-api-key"
```

**Input CSV requirements**:
- Must have a "Business Name" column (case-insensitive matching)
- Other columns are ignored

**Output CSV format**:
- Original Business Name
- First Name (from registered agent)
- Last Name (from registered agent)
- Business Address (formatted mailing address)
- Business Name (official name from franchise tax record)
- Filename: `output_YYYYMMDD_HHMMSS.csv`

## Important Notes

- The application includes rate limiting (200ms between requests) to respect API usage
- Warnings are written to stderr, progress to stdout
- Failed lookups result in blank rows in output (preserves original business name)
- Column matching is case-insensitive with whitespace trimming
- Go version: 1.26.1 (see go.mod)
