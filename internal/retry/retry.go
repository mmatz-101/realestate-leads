package retry

import (
	"fmt"
	"time"
)

// RetryConfig holds configuration for retry logic
type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// DefaultRetryConfig returns the default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 5 * time.Second,
		MaxBackoff:     45 * time.Second,
	}
}

// RateLimitError represents a 429 Too Many Requests error
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded, retry after %v", e.RetryAfter)
}

// WithRetry executes a function with exponential backoff retry logic
func WithRetry(config *RetryConfig, fn func() error) error {
	var err error
	backoff := config.InitialBackoff

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}

		// Check if it's a rate limit error
		if rateLimitErr, ok := err.(*RateLimitError); ok {
			if attempt < config.MaxRetries {
				fmt.Printf("  rate limit hit, waiting %v (attempt %d/%d)...\n",
					backoff, attempt+1, config.MaxRetries)
				time.Sleep(backoff)

				// Exponential backoff: 5s -> 15s -> 45s
				backoff *= 3
				if backoff > config.MaxBackoff {
					backoff = config.MaxBackoff
				}
				continue
			}
			return fmt.Errorf("rate limit exceeded after %d retries: %w", config.MaxRetries, rateLimitErr)
		}

		// Not a rate limit error, return immediately
		return err
	}

	return err
}

// AdaptiveDelay manages adaptive rate limiting
type AdaptiveDelay struct {
	baseDelay     time.Duration
	currentDelay  time.Duration
	adaptiveMode  bool
	adaptiveCount int
	adaptiveLimit int
}

// NewAdaptiveDelay creates a new adaptive delay manager
func NewAdaptiveDelay(baseDelay time.Duration) *AdaptiveDelay {
	return &AdaptiveDelay{
		baseDelay:     baseDelay,
		currentDelay:  baseDelay,
		adaptiveMode:  false,
		adaptiveCount: 0,
		adaptiveLimit: 50, // Process 50 requests at slower rate after 429
	}
}

// Wait sleeps for the current delay duration
func (d *AdaptiveDelay) Wait() {
	time.Sleep(d.currentDelay)
}

// OnRateLimit is called when a 429 error occurs
func (d *AdaptiveDelay) OnRateLimit() {
	if !d.adaptiveMode {
		fmt.Printf("  switching to adaptive mode: increasing delay to 200ms for next %d requests\n", d.adaptiveLimit)
		d.adaptiveMode = true
		d.currentDelay = 200 * time.Millisecond
		d.adaptiveCount = 0
	}
}

// OnSuccess is called after a successful request
func (d *AdaptiveDelay) OnSuccess() {
	if d.adaptiveMode {
		d.adaptiveCount++
		if d.adaptiveCount >= d.adaptiveLimit {
			fmt.Println("  switching back to normal mode: delay reduced to 100ms")
			d.adaptiveMode = false
			d.currentDelay = d.baseDelay
			d.adaptiveCount = 0
		}
	}
}

// CurrentDelay returns the current delay duration
func (d *AdaptiveDelay) CurrentDelay() time.Duration {
	return d.currentDelay
}
