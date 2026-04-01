# Real Estate Lead Enrichment Tool

A Go CLI application that enriches real estate lead data by querying the Texas Comptroller's public franchise tax API. The tool reads business names from a CSV file, looks up their franchise tax records, and outputs enriched data including registered agent names and mailing addresses.

## Features

- **SQLite Cache** - Stores previous lookups to avoid redundant API calls
- **Smart Retry Logic** - Automatically retries failed requests with exponential backoff (5s → 15s → 45s)
- **Adaptive Rate Limiting** - Adjusts delay after rate limit errors (100ms → 200ms)
- **Resume Capability** - Restart failed runs without losing progress
- Automatic franchise tax record lookup for Texas businesses
- Extracts registered agent names and mailing addresses
- Splits agent names into first/last name components
- Handles business name variations (strips LLC, Inc, Corp suffixes for retry)
- Timestamped output files for tracking
- Comprehensive error handling and progress reporting

## Prerequisites

- **Go 1.26.1** or higher
- **Texas Comptroller API Key** - Required for API access

## Installation

### Build from Source

```bash
git clone <repository-url>
cd realestate-leads
go build -o realestate-leads
```

## Configuration

### Required Environment Variable

```bash
export TX_COMPTROLLER_API_KEY="your-api-key-here"
```

To obtain an API key, visit the [Texas Comptroller Public Data Portal](https://comptroller.texas.gov).

## Usage

### Basic Command

```bash
./realestate-leads --file input.csv
```

### Running Without Building

```bash
go run main.go --file input.csv
```

## Input File Format

The input CSV file **must** contain a column named **"Business Name"** (case-insensitive).

### Required Column

| Column Name   | Description                           | Required |
|---------------|---------------------------------------|----------|
| Business Name | The name of the business to look up   | **Yes**  |

### Example Input CSV

```csv
Location,Address,Business Name,First,Last,Number,Notes
Fort Worth,,Sme Homes Llc,Sergio,Vargas,469-235-7395,text
Fort Worth,,Dfw Home Solutions Llc,Joanne,Thorburn,817-225-5647,Auto voicemail
Fate/Royse,3005 Box Elder Ave,GFO Home LLC,Glenn,Gehan,214-908-1350,
```

### Important: What Data is Used?

**Only the "Business Name" column is extracted from your input file.** All other columns (Location, Address, First, Last, Number, Notes, etc.) are completely ignored and **do NOT appear in the output**.

The output file is built entirely from:
- The original "Business Name" value from your input (preserved for reference)
- Data retrieved from the Texas Comptroller's franchise tax API

## Output File Format

The application generates a timestamped CSV file with the following format:

**Filename**: `output_YYYYMMDD_HHMMSS.csv`

### Output Columns

The output CSV contains **only these 5 columns** - no input columns are passed through:

| Column Name            | Source                                                       | Description                                                  |
|------------------------|--------------------------------------------------------------|--------------------------------------------------------------|
| Original Business Name | Input CSV "Business Name" column                             | The business name from your input file (preserved for reference) |
| First Name             | API - `registeredAgentName` (parsed)                         | First name extracted from the registered agent               |
| Last Name              | API - `registeredAgentName` (parsed)                         | Last name extracted from the registered agent                |
| Business Address       | API - `mailingAddress*` fields (formatted)                   | Formatted mailing address from franchise tax records         |
| Business Name          | API - `name` field                                           | Official business name from franchise tax records            |

**Note**: Input columns like Location, Address, First, Last, Number, Notes, etc. are **not** included in the output.

### Example Output CSV

```csv
Original Business Name,First Name,Last Name,Business Address,Business Name
Sme Homes Llc,John,Doe,"123 Main St, Dallas, TX 75201",SME HOMES LLC
Dfw Home Solutions Llc,Jane,Smith,"456 Oak Ave, Fort Worth, TX 76102",DFW HOME SOLUTIONS LLC
GFO Home LLC,Glenn,Gehan,"789 Elm Dr, Austin, TX 78701",GFO HOME LLC
```

### Failed Lookups

If a business cannot be found in the franchise tax database, a row with blank fields will be written:

```csv
Original Business Name,First Name,Last Name,Business Address,Business Name
Unknown Business Inc,,,,,
```

## How It Works

1. **Cache Initialization**: Opens/creates SQLite database (`franchise_tax_cache.db`) to store lookups
2. **Input Validation**: Reads the CSV file and verifies the "Business Name" column exists
3. **Cache Lookup**: For each business name:
   - Checks if data exists in cache (instant lookup)
   - If cached, uses stored data and skips API call
4. **API Lookup** (if not cached):
   - Queries the Texas Comptroller's franchise tax API with retry logic
   - If 429 (rate limit) error occurs:
     - Retries with exponential backoff (5s, 15s, 45s)
     - Switches to adaptive mode (increases delay to 200ms for 50 requests)
   - If no results found, retries with business suffixes stripped (LLC, Inc, Corp, etc.)
5. **Detail Retrieval**: Fetches complete franchise tax record including:
   - Registered agent name
   - Mailing address (street, city, state, ZIP)
6. **Cache Storage**: Saves successful (and failed) lookups to cache for future runs
7. **Data Transformation**:
   - Splits registered agent name into first/last components
   - Formats address components into a single address string
8. **Output Generation**: Writes enriched data to timestamped CSV file

## Rate Limiting & Retry Logic

The application includes intelligent rate limiting and retry mechanisms:

### Base Rate Limiting
- **100ms delay** between API requests under normal conditions
- **200ms delay** in adaptive mode (triggered after 429 errors)
- Adaptive mode lasts for 50 requests before returning to normal

### Exponential Backoff for 429 Errors
When the API returns HTTP 429 (Too Many Requests):
1. **1st retry**: Wait 5 seconds
2. **2nd retry**: Wait 15 seconds
3. **3rd retry**: Wait 45 seconds
4. **After 3 retries**: Mark as failed and continue

This ensures the application respects API rate limits while maximizing throughput.

## Error Handling

- **Missing "Business Name" column**: Application exits with error
- **Empty business names**: Row is skipped with warning
- **API lookup failures**: Warning is logged, blank row is written to output
- **Network errors**: Detailed error messages are written to stderr

### Warning Messages

All warnings are written to **stderr** while progress is written to **stdout**. Example:

```
warning: row 5 ("Unknown Business"): no search results found
```

## SQLite Cache

The application uses a local SQLite database (`franchise_tax_cache.db`) to cache all API lookups:

### What Gets Cached?
- **Successful lookups**: Full franchise tax records with agent and address data
- **Failed lookups**: "Not found" results to avoid repeated API calls for non-existent businesses
- **Timestamp**: Each entry includes when it was cached

### Cache Benefits
- **Speed**: Instant lookups for previously processed businesses
- **Reliability**: Resume interrupted runs without losing progress
- **API Efficiency**: Avoid redundant API calls for duplicate business names
- **Cost Savings**: Reduce API usage if rate limits or quotas apply

### Cache Management

**View cache stats**:
The application displays cache statistics at startup and completion:
```
cache: 439 entries (23 not-found)
```

**Clear cache** (if needed):
```bash
rm franchise_tax_cache.db
```

**Cache location**: Same directory as the executable (`franchise_tax_cache.db`)

## Business Name Suffix Handling

If the initial search returns no results, the application automatically retries with common business suffixes removed:

- LLC, L.L.C., L.L.C
- Inc., Inc, Incorporated
- Corp., Corp, Corporation
- Co., Co, Company
- Ltd., Ltd, Limited
- L.P., LP

**Example**: "Acme Solutions LLC" → retry as "Acme Solutions"

## Development

### Code Formatting

```bash
go fmt main.go
```

### Project Structure

```
realestate-leads/
├── main.go                     # Main application logic and CLI
├── api.go                      # API client and Texas Comptroller integration
├── cache.go                    # SQLite cache implementation
├── retry.go                    # Retry logic and adaptive delay
├── utils.go                    # Utility functions (formatting, parsing)
├── go.mod                      # Go module definition
├── franchise_tax_cache.db      # SQLite cache database (auto-created)
├── CLAUDE.md                   # Development documentation
└── README.md                   # This file
```

### Architecture Overview

The application is modular with clean separation of concerns:

**main.go** - CLI and orchestration
- Command-line argument parsing
- CSV reading/writing
- Main processing loop

**api.go** - API client (`api.go:33-119`)
- `APIClient.SearchFranchiseTax()` - Searches franchise tax records by business name
- `APIClient.GetFranchiseTaxDetail()` - Retrieves detailed tax record by taxpayer ID
- `stripBusinessSuffix()` - Removes common business suffixes for retry logic
- `RateLimitError` - Custom error type for 429 handling

**cache.go** - SQLite caching (`cache.go:16-156`)
- `Cache.Get()` - Retrieves cached entries
- `Cache.Put()` - Stores successful and failed lookups
- `Cache.Stats()` - Returns cache statistics
- Stores both found and not-found results

**retry.go** - Retry and adaptive delay (`retry.go:9-97`)
- `WithRetry()` - Executes functions with exponential backoff
- `AdaptiveDelay` - Manages dynamic rate limiting (100ms → 200ms)
- Handles 429 errors gracefully

**utils.go** - Helper functions (`utils.go:7-41`)
- `splitName()` - Parses registered agent name into first/last components
- `formatAddress()` - Combines address components into formatted string
- `columnIndex()` - Case-insensitive column lookup

## API Details

### Base URL
```
https://api.comptroller.texas.gov
```

### Endpoints Used

1. **Search Franchise Tax Records**
   - Endpoint: `/public-data/v1/public/franchise-tax-list?name={business_name}`
   - Method: GET
   - Returns: List of matching taxpayer IDs

2. **Get Franchise Tax Details**
   - Endpoint: `/public-data/v1/public/franchise-tax/{taxpayerID}`
   - Method: GET
   - Returns: Complete franchise tax record with agent and address information

### Authentication

All API requests require the `x-api-key` header with your Texas Comptroller API key.

## Troubleshooting

### Common Issues

| Error | Cause | Solution |
|-------|-------|----------|
| `TX_COMPTROLLER_API_KEY environment variable is not set` | API key not configured | Set the environment variable with your API key |
| `cannot open input file` | File path is incorrect or file doesn't exist | Verify the file path and ensure the file exists |
| `input CSV has no "Business Name" column` | Required column is missing | Add a "Business Name" column to your CSV file |
| `search API returned HTTP 401` | Invalid or expired API key | Verify your API key is correct and active |
| `search API returned HTTP 429` | Rate limit exceeded | Wait and retry; the app includes automatic delays |

### Debugging Tips

- Run the application and observe stdout for progress messages
- Check stderr for warnings about failed lookups
- Verify input CSV format matches requirements (especially "Business Name" column)
- Test with a small sample file first to ensure API key is working

## Performance

### First Run (No Cache)
- **Rate Limit**: 100ms delay between requests = ~10 requests/second
- **Timeout**: 30-second HTTP timeout per request
- **Throughput**: Approximately 600 records per minute (accounting for delays)
- **Time for 100 records**: ~10 minutes (first run)
- **Time for 500 records**: ~50 minutes (first run, may encounter rate limits)

### Subsequent Runs (With Cache)
- **Cache hits**: Instant lookup (no API call)
- **Time for 100 cached records**: < 1 second
- **Time for 500 cached records**: < 5 seconds

### With Rate Limiting (429 Errors)
If you hit rate limits during the first run:
- Application automatically switches to adaptive mode (200ms delay)
- Retries failed requests with exponential backoff
- Total time may increase by 10-20% on first run
- Subsequent runs use cache and are unaffected

## Limitations

- Only works with businesses registered in Texas
- Requires exact or close match on business name for search results
- Dependent on Texas Comptroller API availability and data accuracy
- Network errors or API downtime will cause lookup failures

## License

[Specify your license here]

## Contributing

[Specify contribution guidelines here]

## Support

For issues related to:
- **API Access**: Contact Texas Comptroller support
- **Application Bugs**: [Specify contact or issue tracker]
- **Feature Requests**: [Specify contact or issue tracker]

## Version History

- **v1.0.0** - Initial release with basic franchise tax lookup functionality
