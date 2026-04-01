package cache

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// CacheEntry represents a cached franchise tax lookup
type CacheEntry struct {
	BusinessName             string
	TaxpayerID               string
	OfficialName             string
	RegisteredAgentName      string
	MailingAddressStreet     string
	MailingAddressCity       string
	MailingAddressState      string
	MailingAddressZip        string
	CachedAt                 time.Time
	NotFound                 bool
}

// Cache manages the SQLite database for caching franchise tax lookups
type Cache struct {
	db *sql.DB
}

// NewCache creates a new cache instance and initializes the database
func NewCache(dbPath string) (*Cache, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening cache database: %w", err)
	}

	// Create table if it doesn't exist
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS franchise_tax_cache (
		business_name TEXT PRIMARY KEY,
		taxpayer_id TEXT,
		official_name TEXT,
		registered_agent_name TEXT,
		mailing_address_street TEXT,
		mailing_address_city TEXT,
		mailing_address_state TEXT,
		mailing_address_zip TEXT,
		cached_at DATETIME,
		not_found INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_cached_at ON franchise_tax_cache(cached_at);
	`

	if _, err := db.Exec(createTableSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating cache table: %w", err)
	}

	return &Cache{db: db}, nil
}

// Close closes the database connection
func (c *Cache) Close() error {
	return c.db.Close()
}

// Get retrieves a cached entry for the given business name
// Returns nil if not found or if the entry is older than maxAge
func (c *Cache) Get(businessName string, maxAge time.Duration) (*CacheEntry, error) {
	query := `
	SELECT taxpayer_id, official_name, registered_agent_name,
	       mailing_address_street, mailing_address_city,
	       mailing_address_state, mailing_address_zip,
	       cached_at, not_found
	FROM franchise_tax_cache
	WHERE business_name = ?
	`

	var entry CacheEntry
	entry.BusinessName = businessName

	var cachedAtStr string
	var notFoundInt int

	err := c.db.QueryRow(query, businessName).Scan(
		&entry.TaxpayerID,
		&entry.OfficialName,
		&entry.RegisteredAgentName,
		&entry.MailingAddressStreet,
		&entry.MailingAddressCity,
		&entry.MailingAddressState,
		&entry.MailingAddressZip,
		&cachedAtStr,
		&notFoundInt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Not found in cache
	}
	if err != nil {
		return nil, fmt.Errorf("querying cache: %w", err)
	}

	entry.CachedAt, _ = time.Parse("2006-01-02 15:04:05", cachedAtStr)
	entry.NotFound = notFoundInt == 1

	// Check if entry is expired
	if maxAge > 0 && time.Since(entry.CachedAt) > maxAge {
		return nil, nil // Expired, return as not found
	}

	return &entry, nil
}

// Put stores a cache entry
func (c *Cache) Put(entry *CacheEntry) error {
	query := `
	INSERT OR REPLACE INTO franchise_tax_cache (
		business_name, taxpayer_id, official_name, registered_agent_name,
		mailing_address_street, mailing_address_city,
		mailing_address_state, mailing_address_zip,
		cached_at, not_found
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	notFoundInt := 0
	if entry.NotFound {
		notFoundInt = 1
	}

	_, err := c.db.Exec(query,
		entry.BusinessName,
		entry.TaxpayerID,
		entry.OfficialName,
		entry.RegisteredAgentName,
		entry.MailingAddressStreet,
		entry.MailingAddressCity,
		entry.MailingAddressState,
		entry.MailingAddressZip,
		time.Now().Format("2006-01-02 15:04:05"),
		notFoundInt,
	)

	if err != nil {
		return fmt.Errorf("inserting into cache: %w", err)
	}

	return nil
}

// Stats returns cache statistics
func (c *Cache) Stats() (total int, notFound int, err error) {
	err = c.db.QueryRow("SELECT COUNT(*) FROM franchise_tax_cache").Scan(&total)
	if err != nil {
		return 0, 0, err
	}

	err = c.db.QueryRow("SELECT COUNT(*) FROM franchise_tax_cache WHERE not_found = 1").Scan(&notFound)
	if err != nil {
		return 0, 0, err
	}

	return total, notFound, nil
}
