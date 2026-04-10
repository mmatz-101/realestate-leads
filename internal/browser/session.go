package browser

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

const authFile = "./auth-session.json"

// SessionManager owns the browser instance and keeps the session alive.
type SessionManager struct {
	mu       sync.RWMutex
	browser  *rod.Browser
	page     *rod.Page
	loginURL string
	keepURL  string // lightweight URL to hit for keep-alive

	refreshInterval time.Duration
	stopCh          chan struct{}
	loggedIn        bool
	tokenExpiry     time.Time // when the current token expires
}

// NewSessionManager creates a session manager for the target site.
func NewSessionManager(loginURL, keepURL string, refreshInterval time.Duration) *SessionManager {
	if keepURL == "" {
		keepURL = loginURL
	}
	if refreshInterval == 0 {
		refreshInterval = 30 * time.Minute
	}
	return &SessionManager{
		loginURL:        loginURL,
		keepURL:         keepURL,
		refreshInterval: refreshInterval,
		stopCh:          make(chan struct{}),
	}
}

// Start launches a visible browser window for the user to log in.
func (sm *SessionManager) Start() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	u, err := launcher.New().
		Headless(false).
		UserDataDir("./browser-data").
		Launch()
	if err != nil {
		return fmt.Errorf("failed to launch browser: %w", err)
	}

	sm.browser = rod.New().ControlURL(u)
	if err := sm.browser.Connect(); err != nil {
		return fmt.Errorf("failed to connect to browser: %w", err)
	}

	sm.page, err = sm.browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return fmt.Errorf("failed to create page: %w", err)
	}

	// Navigate to login page first so we're on the right origin for localStorage.
	if err := sm.page.Navigate(sm.loginURL); err != nil {
		return fmt.Errorf("failed to navigate to login: %w", err)
	}
	sm.page.MustWaitLoad()

	// Try to restore a previous session.
	if sm.restoreAuth() {
		log.Println("[session]  Restored previous auth session")
		// Auto-mark as logged in if we successfully restored auth
		// (already holding sm.mu lock from line 51)
		sm.loggedIn = true
		go sm.keepAliveLoop()
		log.Println("[session]  Auto-detected existing login — keep-alive started")
	} else {
		log.Println("[session]  No saved session — please log in at the browser window")
	}

	log.Printf("[session]  Browser opened at %s", sm.loginURL)
	return nil
}

// MarkLoggedIn should be called once the user confirms they have logged in.
func (sm *SessionManager) MarkLoggedIn() {
	sm.mu.Lock()
	sm.loggedIn = true
	sm.mu.Unlock()

	// Save auth right after login confirmation.
	sm.saveAuth()

	go sm.keepAliveLoop()
	log.Println("[session]  User marked as logged in — keep-alive started")
}

// IsLoggedIn returns the current login state.
func (sm *SessionManager) IsLoggedIn() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.loggedIn
}

// Page returns the active browser page for use by the searcher.
func (sm *SessionManager) Page() *rod.Page {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.page
}

// Browser returns the rod browser instance.
func (sm *SessionManager) Browser() *rod.Browser {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.browser
}

// GetToken extracts the bearer token (sessionId) from the saved persist:root data.
func (sm *SessionManager) GetToken() string {
	data, err := os.ReadFile(authFile)
	if err != nil {
		log.Printf("[session]  Failed to read auth file: %v", err)
		return ""
	}

	// persist:root is a JSON object where each value is a JSON-encoded string.
	// We need: persist:root -> authentication (string) -> parse -> session -> sessionId
	var root map[string]string
	if err := json.Unmarshal(data, &root); err != nil {
		// Fallback: search for sessionId UUID in the raw content.
		log.Printf("[session]  Failed to parse persist:root as JSON: %v", err)
		return findUUID(string(data))
	}

	authStr, ok := root["authentication"]
	if !ok || authStr == "" {
		log.Println("[session]  No 'authentication' key in persist:root")
		return ""
	}

	// authStr is itself a JSON string: {"session":{"sessionId":"..."},"status":"succeeded"}
	var authData struct {
		Session struct {
			SessionID string `json:"sessionId"`
		} `json:"session"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(authStr), &authData); err != nil {
		log.Printf("[session]  Failed to parse authentication JSON: %v", err)
		return findUUID(authStr)
	}

	if authData.Session.SessionID == "" {
		log.Println("[session]  sessionId is empty")
		return ""
	}

	log.Printf("[session]  Token extracted: %s...%s",
		authData.Session.SessionID[:8],
		authData.Session.SessionID[len(authData.Session.SessionID)-4:])
	return authData.Session.SessionID
}

// saveAuth saves the entire persist:root from localStorage to disk.
func (sm *SessionManager) saveAuth() {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.browser == nil {
		return
	}

	// Find the page that's on the forewarn origin.
	pages, err := sm.browser.Pages()
	if err != nil {
		log.Printf("[session]  Failed to list pages: %v", err)
		return
	}

	var targetPage *rod.Page
	for _, p := range pages {
		info, err := p.Info()
		if err != nil {
			continue
		}
		if strings.Contains(info.URL, "forewarn.com") {
			targetPage = p
			break
		}
	}

	if targetPage == nil {
		log.Println("[session]  No forewarn tab found")
		return
	}

	result, err := targetPage.Eval(`() => {
		let data = localStorage.getItem("persist:root");
		if (!data) return "NOAUTH";
		return data;
	}`)
	if err != nil {
		log.Printf("[session]  Failed to read localStorage: %v", err)
		return
	}

	auth := fmt.Sprintf("%v", result.Value)
	if auth == "" || auth == "NOAUTH" || auth == "null" {
		log.Println("[session]  No persist:root found in localStorage")
		return
	}

	if err := os.WriteFile(authFile, []byte(auth), 0600); err != nil {
		log.Printf("[session]  Failed to save auth: %v", err)
		return
	}
	log.Println("[session]  Auth saved to disk (persist:root)")
}

// restoreAuth loads the full persist:root from disk into localStorage.
func (sm *SessionManager) restoreAuth() bool {
	data, err := os.ReadFile(authFile)
	if err != nil {
		return false
	}

	auth := string(data)
	if auth == "" || auth == "NOAUTH" {
		return false
	}

	// Restore the entire persist:root.
	sm.page.MustEval(`(data) => localStorage.setItem("persist:root", data)`, auth)
	sm.page.MustReload()
	sm.page.MustWaitLoad()

	log.Println("[session]  Full persist:root restored from disk")
	return true
}

// refreshToken calls the Forewarn refresh API to extend the session.
func (sm *SessionManager) refreshToken() error {
	token := sm.GetToken()
	if token == "" {
		return fmt.Errorf("no token available to refresh")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("POST", "https://api.forewarn.com/api/authentication/refresh", nil)
	if err != nil {
		return fmt.Errorf("failed to create refresh request: %w", err)
	}

	req.Header.Set("Authorization", "bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://app.forewarn.com")
	req.Header.Set("Referer", "https://app.forewarn.com/")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("refresh failed with status %d", resp.StatusCode)
	}

	// Parse the response to get the new session info
	var refreshResp struct {
		SessionID string    `json:"sessionId"`
		Expires   time.Time `json:"expires"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&refreshResp); err != nil {
		return fmt.Errorf("failed to decode refresh response: %w", err)
	}

	// Update the expiry time
	sm.mu.Lock()
	sm.tokenExpiry = refreshResp.Expires
	sm.mu.Unlock()

	log.Printf("[session]  Token refreshed successfully, expires at %s", refreshResp.Expires.Format(time.RFC3339))

	// Re-save auth to capture any updates
	sm.saveAuth()

	return nil
}

// needsRefresh checks if the token is expiring within the next 10 minutes.
func (sm *SessionManager) needsRefresh() bool {
	sm.mu.RLock()
	expiry := sm.tokenExpiry
	sm.mu.RUnlock()

	// If we don't know the expiry time, assume we need refresh
	if expiry.IsZero() {
		return true
	}

	// Refresh if expiring within 10 minutes
	timeUntilExpiry := time.Until(expiry)
	return timeUntilExpiry < 10*time.Minute
}

// keepAliveLoop periodically checks and refreshes the session token via API.
func (sm *SessionManager) keepAliveLoop() {
	ticker := time.NewTicker(sm.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Only refresh if token is expiring within 10 minutes
			if !sm.needsRefresh() {
				sm.mu.RLock()
				expiry := sm.tokenExpiry
				sm.mu.RUnlock()
				timeLeft := time.Until(expiry).Round(time.Minute)
				log.Printf("[session]  Keep-alive: token still fresh (expires in %v), skipping refresh", timeLeft)
				continue
			}

			log.Println("[session]  Keep-alive: token expiring soon, refreshing...")
			if err := sm.refreshToken(); err != nil {
				log.Printf("[session]  Token refresh failed: %v", err)
				// Fallback to page navigation if API refresh fails
				sm.mu.Lock()
				if sm.page != nil {
					log.Println("[session]  Attempting fallback page navigation...")
					if err := sm.page.Navigate(sm.keepURL); err != nil {
						log.Printf("[session]  Fallback navigation also failed: %v", err)
					}
				}
				sm.mu.Unlock()
			}

		case <-sm.stopCh:
			log.Println("[session]  Keep-alive loop stopped")
			return
		}
	}
}

// OpenLocalUI opens the localhost UI in a new browser tab.
func (sm *SessionManager) OpenLocalUI(port int) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.browser == nil {
		return fmt.Errorf("browser not started")
	}

	url := fmt.Sprintf("http://localhost:%d", port)
	_, err := sm.browser.Page(proto.TargetCreateTarget{URL: url})
	if err != nil {
		return fmt.Errorf("failed to open UI tab: %w", err)
	}

	log.Printf("[session]  Opened UI at %s", url)
	return nil
}

// Stop saves auth and shuts down the browser.
func (sm *SessionManager) Stop() {
	// Save auth before closing so we can restore next time.
	sm.saveAuth()

	close(sm.stopCh)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.browser != nil {
		if err := sm.browser.Close(); err != nil {
			log.Printf("[session]  Error closing browser: %v", err)
		}
	}
	log.Println("[session]  Browser closed")
}

// findUUID finds the first UUID-like string in the text.
func findUUID(text string) string {
	re := regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	return re.FindString(text)
}
