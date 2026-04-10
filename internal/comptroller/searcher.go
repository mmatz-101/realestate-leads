package comptroller

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// SearchResult holds the output of a single comptroller search operation.
type SearchResult struct {
	InputRow   map[string]string
	Found      bool
	Skipped    bool
	SkipReason string
	Output     map[string]string
	Error      string
	Timestamp  time.Time
}

// Searcher makes API calls to Texas Comptroller.
type Searcher struct {
	client       *Client
	delayBetween time.Duration
}

// NewSearcher creates a searcher that uses the comptroller API.
func NewSearcher(apiKey string, delayBetween time.Duration) *Searcher {
	if delayBetween == 0 {
		delayBetween = 500 * time.Millisecond // Faster than Forewarn since it's a public API
	}
	return &Searcher{
		client:       NewClient(apiKey),
		delayBetween: delayBetween,
	}
}

// Search performs a single Comptroller API search for a business.
func (s *Searcher) Search(inputRow map[string]string) SearchResult {
	result := SearchResult{
		InputRow:  inputRow,
		Timestamp: time.Now(),
		Output:    make(map[string]string),
	}

	businessName := strings.TrimSpace(inputRow["Business Name"])

	// Skip rows with no business name.
	if businessName == "" {
		result.Skipped = true
		result.SkipReason = "no business name provided"
		result.Output["status"] = "skipped — no business name"
		log.Printf("[comptroller] Skipping row: no business name")
		return result
	}

	log.Printf("[comptroller] Searching: %s", businessName)

	// Search for taxpayer ID.
	taxpayerID, err := s.client.SearchFranchiseTax(businessName)
	if err != nil {
		result.Error = fmt.Sprintf("search failed: %v", err)
		log.Printf("[comptroller] ⚠️  Search failed for %s: %v", businessName, err)
		return result
	}

	if taxpayerID == "" {
		result.Found = false
		result.Output["status"] = "not found"
		log.Printf("[comptroller] No results for %s", businessName)
		return result
	}

	// Get details.
	detail, err := s.client.GetFranchiseTaxDetail(taxpayerID)
	if err != nil {
		result.Error = fmt.Sprintf("detail lookup failed: %v", err)
		log.Printf("[comptroller] ⚠️  Detail lookup failed for %s: %v", businessName, err)
		return result
	}

	// Extract data.
	d := detail.Data
	firstName, lastName := SplitName(d.RegisteredAgentName)
	address := FormatAddress(
		d.MailingAddressStreet,
		d.MailingAddressCity,
		d.MailingAddressState,
		d.MailingAddressZip,
	)

	result.Found = true
	result.Output["status"] = "found"
	result.Output["First Name"] = firstName
	result.Output["Last Name"] = lastName
	result.Output["Business Address"] = address
	result.Output["Business Name"] = d.Name

	log.Printf("[comptroller] ✓ %s — agent: %s %s", businessName, firstName, lastName)

	return result
}

// SearchAll runs searches for all rows and sends results to the channel.
func (s *Searcher) SearchAll(rows []map[string]string, results chan<- SearchResult, cancelCh <-chan struct{}) {
	defer close(results)

	for i, row := range rows {
		// Check if cancelled before processing next row.
		select {
		case <-cancelCh:
			log.Printf("[comptroller] Cancelled at row %d/%d", i+1, len(rows))
			return
		default:
		}

		log.Printf("[comptroller] Processing row %d/%d", i+1, len(rows))
		result := s.Search(row)
		results <- result

		if i < len(rows)-1 {
			log.Printf("[comptroller] Waiting %.1fs before next search", s.delayBetween.Seconds())
			time.Sleep(s.delayBetween)
		}
	}
}
