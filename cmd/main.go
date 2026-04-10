package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mmatz-101/realestate-leads/internal/browser"
	"github.com/mmatz-101/realestate-leads/internal/comptroller"
	"github.com/mmatz-101/realestate-leads/internal/jobs"
	"github.com/mmatz-101/realestate-leads/internal/server"
)

func main() {
	// CLI flags
	loginURL := "https://app.forewarn.com/login"
	searchURL := "https://app.forewarn.com/search"
	keepAliveURL := "https://app.forewarn.com/recent-searches"
	port := flag.Int("port", 8080, "Port for the local web UI")
	refreshMin := flag.Int("refresh", 30, "Session refresh interval in minutes")
	delayMs := flag.Int("delay", 2000, "Delay between searches in milliseconds")
	flag.Parse()

	log.Println("=== realestate-leads ===")
	log.Printf("Web UI:       http://localhost:%d", *port)
	log.Printf("Refresh:      every %d minutes", *refreshMin)
	log.Printf("Search delay: %dms", *delayMs)
	log.Println()

	// 1. Start the session manager — opens a visible browser for login.
	session := browser.NewSessionManager(loginURL, *&keepAliveURL, time.Duration(*refreshMin)*time.Minute)
	if err := session.Start(); err != nil {
		log.Fatalf("Failed to start session: %v", err)
	}
	defer session.Stop()

	// 2. Create the searchers and job runner.
	forewarnSearcher := browser.NewSearcher(session, searchURL, time.Duration(*delayMs)*time.Millisecond)

	// Get comptroller API key from environment
	apiKey := os.Getenv("TX_COMPTROLLER_API_KEY")
	if apiKey == "" {
		log.Println("Warning: TX_COMPTROLLER_API_KEY not set - preview/full modes will not work")
	}
	comptrollerSearcher := comptroller.NewSearcher(apiKey, 500*time.Millisecond)

	runner := jobs.NewRunner(forewarnSearcher, comptrollerSearcher)

	// 3. Start the HTTP server.
	srv := server.NewServer(session, runner, *port)

	// Handle graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("\nShutting down...")
		session.Stop()
		os.Exit(0)
	}()

	// Start server in a goroutine so we can open the UI after
	go func() {
		if err := srv.Start(); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Give the server a moment to start, then open the localhost UI
	time.Sleep(500 * time.Millisecond)
	if err := session.OpenLocalUI(*port); err != nil {
		log.Printf("Failed to open UI automatically: %v", err)
		log.Printf("Please manually open http://localhost:%d in your browser", *port)
	}

	// Block forever (server runs in background goroutine)
	select {}
}
