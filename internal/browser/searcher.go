package browser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const forewarnAPI = "https://api.forewarn.com/api/search"

// SearchResult holds the output of a single search operation.
type SearchResult struct {
	InputRow   map[string]string
	Found      bool
	Skipped    bool
	SkipReason string
	Output     map[string]string
	Error      string
	Timestamp  time.Time
}

// Searcher makes API calls to Forewarn.
type Searcher struct {
	session      *SessionManager
	client       *http.Client
	delayBetween time.Duration
}

// NewSearcher creates a searcher that uses the session's auth token.
func NewSearcher(session *SessionManager, searchURL string, delayBetween time.Duration) *Searcher {
	if delayBetween == 0 {
		delayBetween = 2 * time.Second
	}
	return &Searcher{
		session:      session,
		client:       &http.Client{Timeout: 30 * time.Second},
		delayBetween: delayBetween,
	}
}

// --- Forewarn API types ---

type searchRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Zip       string `json:"zip,omitempty"`
}

type forewarnResponse struct {
	Result []forewarnPerson `json:"result"`
}

type forewarnPerson struct {
	Name     []nameEntry     `json:"name"`
	DOB      []dobEntry      `json:"dob"`
	Phone    []phoneEntry    `json:"phone"`
	Property []propertyEntry `json:"property"`
	IsDead   bool            `json:"isDead"`
	PID      string          `json:"pid"`
	Address  []addressEntry  `json:"address"`
}

type nameEntry struct {
	Data string `json:"data"`
}

type dobEntry struct {
	Age string `json:"age"`
}

type phoneEntry struct {
	Meta   phoneMeta `json:"meta"`
	Type   string    `json:"type"`
	Number string    `json:"number"`
}

type phoneMeta struct {
	LastSeen int `json:"lastSeen"`
}

type propertyEntry struct {
	Owner   []ownerEntry   `json:"owner"`
	Address propertyAddr   `json:"address"`
	History []historyEntry `json:"history"`
}

type ownerEntry struct {
	Name string `json:"name"`
}

type propertyAddr struct {
	City     string `json:"city"`
	State    string `json:"state"`
	Zip      string `json:"zip"`
	Complete string `json:"complete"`
}

type historyEntry struct {
	Buyer  []buyerEntry `json:"buyer"`
	Detail detailEntry  `json:"detail"`
}

type buyerEntry struct {
	Name string `json:"name"`
}

type detailEntry struct {
	TransferDate dateField `json:"transferDate"`
	SalesPrice   int       `json:"salesPrice,omitempty"`
}

type dateField struct {
	Data string `json:"data"`
}

type addressEntry struct {
	City      string `json:"city"`
	State     string `json:"state"`
	Zip       string `json:"zip"`
	Complete  string `json:"complete"`
	DateRange string `json:"dateRange"`
}

// Search performs a single Forewarn API search for a person.
func (s *Searcher) Search(inputRow map[string]string) SearchResult {
	result := SearchResult{
		InputRow:  inputRow,
		Timestamp: time.Now(),
		Output:    make(map[string]string),
	}

	firstName := strings.TrimSpace(inputRow["First Name"])
	lastName := strings.TrimSpace(inputRow["Last Name"])

	// Skip rows with no name.
	if firstName == "" && lastName == "" {
		result.Skipped = true
		result.SkipReason = "no name provided"
		result.Output["status"] = "skipped — no name"
		log.Printf("[searcher] Skipping row: no name (business: %s)", inputRow["Original Business Name"])
		return result
	}

	zip := extractZip(inputRow["Business Address"])

	// Get the bearer token from the session.
	token := s.session.GetToken()
	if token == "" {
		result.Error = "no auth token available"
		return result
	}

	// Build the API request.
	reqBody := searchRequest{
		FirstName: firstName,
		LastName:  lastName,
		Zip:       zip,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		result.Error = fmt.Sprintf("failed to marshal request: %v", err)
		return result
	}

	req, err := http.NewRequest("POST", forewarnAPI, bytes.NewReader(bodyBytes))
	if err != nil {
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		return result
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "bearer "+token)
	req.Header.Set("Origin", "https://app.forewarn.com")
	req.Header.Set("Referer", "https://app.forewarn.com/")

	log.Printf("[searcher] Searching: %s %s (zip: %s)", firstName, lastName, zip)

	resp, err := s.client.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("API request failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	// Check for authentication errors
	if resp.StatusCode == http.StatusUnauthorized {
		log.Printf("[searcher] ⚠️  Token expired (401) - session needs re-authentication")
		result.Error = "Authentication expired - please re-login"
		return result
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		result.Error = fmt.Sprintf("API returned %d: %s", resp.StatusCode, string(body))
		return result
	}

	var fwResp forewarnResponse
	if err := json.NewDecoder(resp.Body).Decode(&fwResp); err != nil {
		result.Error = fmt.Sprintf("failed to decode response: %v", err)
		return result
	}

	if len(fwResp.Result) == 0 {
		result.Found = false
		result.Output["status"] = "no results"
		log.Printf("[searcher] No results for %s %s", firstName, lastName)
		return result
	}

	// Use the first result.
	person := fwResp.Result[0]

	// Check deceased.
	if person.IsDead {
		result.Skipped = true
		result.SkipReason = "deceased"
		result.Output["status"] = "skipped — deceased"
		log.Printf("[searcher] %s %s — deceased, skipping", firstName, lastName)
		return result
	}

	// Check property count.
	propCount := len(person.Property)
	result.Output["property_count"] = strconv.Itoa(propCount)
	if propCount < 5 {
		result.Skipped = true
		result.SkipReason = fmt.Sprintf("only %d properties (need 5+)", propCount)
		result.Output["status"] = fmt.Sprintf("skipped — %d properties", propCount)
		log.Printf("[searcher] %s %s — %d properties, skipping", firstName, lastName, propCount)
		return result
	}

	// Extract most recent property purchase date.
	lastPurchaseDate := ""
	for _, prop := range person.Property {
		if len(prop.History) > 0 {
			// History entries should be sorted with most recent first
			transferDate := prop.History[0].Detail.TransferDate.Data
			if transferDate != "" {
				lastPurchaseDate = transferDate
				break
			}
		}
	}

	// Extract phone numbers.
	var mobilePhone, mobileLastSeen string
	var residentialPhone, residentialLastSeen string

	for _, phone := range person.Phone {
		lastSeen := formatLastSeen(phone.Meta.LastSeen)

		if strings.EqualFold(phone.Type, "Mobile") && mobilePhone == "" {
			mobilePhone = phone.Number
			mobileLastSeen = lastSeen
		}
		if strings.EqualFold(phone.Type, "Residential") && residentialPhone == "" {
			residentialPhone = phone.Number
			residentialLastSeen = lastSeen
		}
		if mobilePhone != "" && residentialPhone != "" {
			break
		}
	}

	// Get primary name.
	primaryName := ""
	if len(person.Name) > 0 {
		primaryName = person.Name[0].Data
	}

	// Get age.
	age := ""
	if len(person.DOB) > 0 {
		age = person.DOB[0].Age
	}

	// Get current address.
	currentAddress := ""
	if len(person.Address) > 0 {
		a := person.Address[0]
		currentAddress = fmt.Sprintf("%s, %s, %s %s", a.Complete, a.City, a.State, a.Zip)
	}

	result.Found = true
	result.Output["status"] = "found"
	result.Output["full_name"] = primaryName
	result.Output["age"] = age
	result.Output["current_address"] = currentAddress
	result.Output["mobile_phone"] = mobilePhone
	result.Output["mobile_last_seen"] = mobileLastSeen
	result.Output["residential_phone"] = residentialPhone
	result.Output["residential_last_seen"] = residentialLastSeen
	result.Output["property_count"] = strconv.Itoa(propCount)
	result.Output["last_purchase_date"] = lastPurchaseDate

	log.Printf("[searcher] ✓ %s %s — mobile: %s, residential: %s, properties: %d",
		firstName, lastName, mobilePhone, residentialPhone, propCount)

	return result
}

// randomDelay returns a delay with random jitter applied.
// Jitter is ±25% of the base delay to make requests appear more human-like.
func (s *Searcher) randomDelay() time.Duration {
	baseDelay := s.delayBetween

	// Calculate jitter as ±25% of base delay
	maxJitter := float64(baseDelay) * 0.25
	jitter := (rand.Float64()*2 - 1) * maxJitter // Random value between -maxJitter and +maxJitter

	delay := time.Duration(float64(baseDelay) + jitter)

	// Ensure minimum delay of 1 second to avoid rate limiting
	minDelay := 1 * time.Second
	if delay < minDelay {
		delay = minDelay
	}

	return delay
}

// SearchAll runs searches for all rows and sends results to the channel.
func (s *Searcher) SearchAll(rows []map[string]string, results chan<- SearchResult, cancelCh <-chan struct{}) {
	defer close(results)

	for i, row := range rows {
		// Check if cancelled before processing next row
		select {
		case <-cancelCh:
			log.Printf("[searcher] Cancelled at row %d/%d", i+1, len(rows))
			return
		default:
		}

		log.Printf("[searcher] Processing row %d/%d", i+1, len(rows))
		result := s.Search(row)
		results <- result

		if i < len(rows)-1 {
			// Apply random delay with jitter for human-like timing
			delay := s.randomDelay()
			log.Printf("[searcher] Waiting %.1fs before next search", delay.Seconds())
			time.Sleep(delay)
		}
	}
}

// --- Helpers ---

var zipRegex = regexp.MustCompile(`(\d{5})(?:\s*$)`)

func extractZip(address string) string {
	matches := zipRegex.FindStringSubmatch(address)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// formatLastSeen converts 20260128 -> "01/28/2026"
func formatLastSeen(raw int) string {
	if raw == 0 {
		return ""
	}
	s := strconv.Itoa(raw)
	if len(s) != 8 {
		return s
	}
	return fmt.Sprintf("%s/%s/%s", s[4:6], s[6:8], s[0:4])
}
