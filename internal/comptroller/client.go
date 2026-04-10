package comptroller

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	baseURL     = "https://api.comptroller.texas.gov"
	httpTimeout = 30 * time.Second
)

type searchResponse struct {
	Success bool `json:"success"`
	Count   int  `json:"count"`
	Data    []struct {
		TaxpayerID string `json:"taxpayerId"`
		Name       string `json:"name"`
		FileNumber string `json:"fileNumber"`
	} `json:"data"`
}

// DetailResponse holds business details from the Texas Comptroller API.
type DetailResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Name                 string `json:"name"`
		MailingAddressStreet string `json:"mailingAddressStreet"`
		MailingAddressCity   string `json:"mailingAddressCity"`
		MailingAddressState  string `json:"mailingAddressState"`
		MailingAddressZip    string `json:"mailingAddressZip"`
		RegisteredAgentName  string `json:"registeredAgentName"`
	} `json:"data"`
}

// Client handles communication with the Texas Comptroller API.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new API client.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

// apiGet performs a GET request to the API.
func (c *Client) apiGet(endpoint string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("x-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", err)
	}
	return body, resp.StatusCode, nil
}

// SearchFranchiseTax searches for a business by name and returns the taxpayer ID.
func (c *Client) SearchFranchiseTax(name string) (string, error) {
	endpoint := "/public-data/v1/public/franchise-tax-list?name=" + url.QueryEscape(name)
	body, status, err := c.apiGet(endpoint)
	if err != nil {
		return "", err
	}
	if status == http.StatusTooManyRequests {
		return "", fmt.Errorf("rate limited (429)")
	}
	if status != http.StatusOK {
		snippet := body
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return "", fmt.Errorf("search API returned HTTP %d: %s", status, snippet)
	}

	var result searchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing search response: %w", err)
	}
	if result.Count == 0 || len(result.Data) == 0 {
		// Try again without business suffix (LLC, Inc, Corp, etc.)
		stripped := stripBusinessSuffix(name)
		if stripped != "" {
			return c.SearchFranchiseTax(stripped)
		}
		return "", nil
	}
	return result.Data[0].TaxpayerID, nil
}

// GetFranchiseTaxDetail retrieves detailed information for a taxpayer ID.
func (c *Client) GetFranchiseTaxDetail(taxpayerID string) (*DetailResponse, error) {
	endpoint := "/public-data/v1/public/franchise-tax/" + url.PathEscape(taxpayerID)
	body, status, err := c.apiGet(endpoint)
	if err != nil {
		return nil, err
	}
	if status == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited (429)")
	}
	if status != http.StatusOK {
		snippet := body
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("detail API returned HTTP %d: %s", status, snippet)
	}

	var result DetailResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing detail response: %w", err)
	}
	return &result, nil
}

// stripBusinessSuffix removes common business suffixes for retry logic.
func stripBusinessSuffix(name string) string {
	suffixes := []string{
		" LLC", " L.L.C.", " L.L.C",
		" Inc.", " Inc", " Incorporated",
		" Corp.", " Corp", " Corporation",
		" Co.", " Co", " Company",
		" Ltd.", " Ltd", " Limited",
		" L.P.", " LP",
	}
	nameUpper := strings.ToUpper(name)

	for _, suffix := range suffixes {
		suffixUpper := strings.ToUpper(suffix)
		if strings.HasSuffix(nameUpper, suffixUpper) {
			return strings.TrimSpace(name[:len(name)-len(suffix)])
		}
	}
	return ""
}

// SplitName splits "John Doe" into ("John", "Doe").
// Handles edge cases like single names or empty strings.
func SplitName(fullName string) (string, string) {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return "", ""
	}
	parts := strings.Fields(fullName)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[len(parts)-1]
}

// FormatAddress combines address components into a single string.
func FormatAddress(street, city, state, zip string) string {
	parts := []string{}
	if street != "" {
		parts = append(parts, street)
	}
	if city != "" {
		parts = append(parts, city)
	}
	if state != "" {
		parts = append(parts, state)
	}
	if zip != "" {
		parts = append(parts, zip)
	}
	return strings.Join(parts, ", ")
}
