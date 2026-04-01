package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mmatz-101/realestate-leads/internal/api"
	"github.com/mmatz-101/realestate-leads/internal/cache"
	"github.com/mmatz-101/realestate-leads/internal/retry"
	"github.com/mmatz-101/realestate-leads/internal/utils"
)

const (
	requestDelay   = 100 * time.Millisecond
	cachePath      = "franchise_tax_cache.db"
	cacheExpiry    = 2 * time.Hour // Cache entries expire after 2 hours
)

func main() {
	filePath := flag.String("file", "", "Path to the input CSV file (required)")
	flag.Parse()

	if *filePath == "" {
		fmt.Fprintln(os.Stderr, "error: --file flag is required")
		flag.Usage()
		os.Exit(1)
	}

	apiKey := os.Getenv("TX_COMPTROLLER_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: TX_COMPTROLLER_API_KEY environment variable is not set")
		os.Exit(1)
	}

	// Initialize cache
	cacheDB, err := cache.NewCache(cachePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot initialize cache: %v\n", err)
		os.Exit(1)
	}
	defer cacheDB.Close()

	// Show cache stats
	totalCached, notFoundCached, _ := cacheDB.Stats()
	fmt.Printf("cache: %d entries (%d not-found), expiry: %v\n", totalCached, notFoundCached, cacheExpiry)

	// Initialize API client
	apiClient := api.NewClient(apiKey)

	// Initialize adaptive delay
	adaptiveDelay := retry.NewAdaptiveDelay(requestDelay)

	// Initialize retry config
	retryConfig := retry.DefaultRetryConfig()

	// Open input file
	inFile, err := os.Open(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot open input file: %v\n", err)
		os.Exit(1)
	}
	defer inFile.Close()

	reader := csv.NewReader(inFile)
	reader.TrimLeadingSpace = true

	headers, err := reader.Read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot read CSV headers: %v\n", err)
		os.Exit(1)
	}

	bizNameIdx := utils.ColumnIndex(headers, "Business Name")
	if bizNameIdx == -1 {
		fmt.Fprintf(os.Stderr, "error: input CSV has no \"Business Name\" column (found headers: %v)\n", headers)
		os.Exit(1)
	}

	// Create output file
	outName := utils.OutputFilename()
	outFile, err := os.Create(outName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot create output file: %v\n", err)
		os.Exit(1)
	}
	defer outFile.Close()

	writer := csv.NewWriter(outFile)
	defer writer.Flush()

	if err := writer.Write([]string{
		"Original Business Name",
		"First Name",
		"Last Name",
		"Business Address",
		"Business Name",
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: writing output header: %v\n", err)
		os.Exit(1)
	}

	var processed, warnings, cacheHits, notFound int
	rowNum := 1

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: row %d: skipping unreadable CSV row: %v\n", rowNum, err)
			warnings++
			rowNum++
			continue
		}

		if bizNameIdx >= len(record) {
			fmt.Fprintf(os.Stderr, "warning: row %d: \"Business Name\" column missing in this row, skipping\n", rowNum)
			warnings++
			rowNum++
			continue
		}

		originalName := strings.TrimSpace(record[bizNameIdx])
		if originalName == "" {
			fmt.Fprintf(os.Stderr, "warning: row %d: empty business name, skipping\n", rowNum)
			warnings++
			rowNum++
			continue
		}

		fmt.Printf("row %d: processing %q ...\n", rowNum, originalName)

		// Check cache first (with 2-hour expiry)
		cached, err := cacheDB.Get(originalName, cacheExpiry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: row %d: cache lookup error: %v\n", rowNum, err)
		}

		if cached != nil {
			cacheHits++
			fmt.Printf("  cache hit!\n")

			if cached.NotFound {
				fmt.Fprintf(os.Stderr, "warning: row %d (%q): not found (cached)\n", rowNum, originalName)
				notFound++
				writeBlankRow(writer, originalName)
				rowNum++
				continue
			}

			// Use cached data
			first, last := utils.SplitName(cached.RegisteredAgentName)
			address := utils.FormatAddress(
				cached.MailingAddressStreet,
				cached.MailingAddressCity,
				cached.MailingAddressState,
				cached.MailingAddressZip,
			)

			if err := writer.Write([]string{
				originalName,
				first,
				last,
				address,
				cached.OfficialName,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "error: row %d: writing output row: %v\n", rowNum, err)
			}

			processed++
			rowNum++
			continue
		}

		// Not in cache, fetch from API with retry logic
		var taxpayerID string
		var detail *api.DetailResponse
		fetchSuccess := false

		// Search for taxpayer ID with retry
		err = retry.WithRetry(retryConfig, func() error {
			var searchErr error
			taxpayerID, searchErr = apiClient.SearchFranchiseTax(originalName)
			return searchErr
		})

		if err != nil {
			if _, isRateLimit := err.(*retry.RateLimitError); isRateLimit {
				adaptiveDelay.OnRateLimit()
			}
			fmt.Fprintf(os.Stderr, "warning: row %d (%q): search failed: %v\n", rowNum, originalName, err)
			warnings++
			writeBlankRow(writer, originalName)
			adaptiveDelay.Wait()
			rowNum++
			continue
		}

		if taxpayerID == "" {
			// Not found - cache this result
			fmt.Fprintf(os.Stderr, "warning: row %d (%q): no search results found\n", rowNum, originalName)
			notFound++
			cacheDB.Put(&cache.CacheEntry{
				BusinessName: originalName,
				NotFound:     true,
			})
			writeBlankRow(writer, originalName)
			adaptiveDelay.Wait()
			rowNum++
			continue
		}

		// Get details with retry
		err = retry.WithRetry(retryConfig, func() error {
			var detailErr error
			detail, detailErr = apiClient.GetFranchiseTaxDetail(taxpayerID)
			return detailErr
		})

		if err != nil {
			if _, isRateLimit := err.(*retry.RateLimitError); isRateLimit {
				adaptiveDelay.OnRateLimit()
			}
			fmt.Fprintf(os.Stderr, "warning: row %d (%q): detail lookup failed: %v\n", rowNum, originalName, err)
			warnings++
			writeBlankRow(writer, originalName)
			adaptiveDelay.Wait()
			rowNum++
			continue
		}

		// Success! Cache the result
		d := detail.Data
		cacheEntry := &cache.CacheEntry{
			BusinessName:         originalName,
			TaxpayerID:           taxpayerID,
			OfficialName:         d.Name,
			RegisteredAgentName:  d.RegisteredAgentName,
			MailingAddressStreet: d.MailingAddressStreet,
			MailingAddressCity:   d.MailingAddressCity,
			MailingAddressState:  d.MailingAddressState,
			MailingAddressZip:    d.MailingAddressZip,
			NotFound:             false,
		}

		if err := cacheDB.Put(cacheEntry); err != nil {
			fmt.Fprintf(os.Stderr, "warning: row %d: failed to cache result: %v\n", rowNum, err)
		}

		first, last := utils.SplitName(d.RegisteredAgentName)
		address := utils.FormatAddress(
			d.MailingAddressStreet,
			d.MailingAddressCity,
			d.MailingAddressState,
			d.MailingAddressZip,
		)

		if err := writer.Write([]string{
			originalName,
			first,
			last,
			address,
			d.Name,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "error: row %d: writing output row: %v\n", rowNum, err)
		}

		processed++
		fetchSuccess = true

		// Update adaptive delay on success
		if fetchSuccess {
			adaptiveDelay.OnSuccess()
		}

		adaptiveDelay.Wait()
		rowNum++
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		fmt.Fprintf(os.Stderr, "error: flushing output CSV: %v\n", err)
	}

	fmt.Printf("\ndone — %d rows processed, %d not found, %d errors, %d cache hits\n", processed, notFound, warnings, cacheHits)
	fmt.Printf("output written to: %s\n", outName)

	// Show final cache stats
	totalCached, notFoundCached, _ = cacheDB.Stats()
	fmt.Printf("cache now contains: %d entries (%d not-found)\n", totalCached, notFoundCached)
}

func writeBlankRow(w *csv.Writer, originalName string) {
	_ = w.Write([]string{originalName, "", "", "", ""})
}
