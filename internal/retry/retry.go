// Package retry provides exponential backoff for transient API errors.
package retry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const MaxRetries = 3

// IsRetryableStatus returns true for transient HTTP status codes.
func IsRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusBadGateway ||
		statusCode == http.StatusServiceUnavailable ||
		statusCode == http.StatusGatewayTimeout ||
		statusCode == http.StatusTooManyRequests
}

// Do executes fn with exponential backoff on transient errors.
// shouldRetry inspects the error and returns true if it's retryable.
func Do(ctx context.Context, label string, fn func() error, shouldRetry func(error) bool) error {
	var lastErr error
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		if !shouldRetry(err) {
			return err
		}

		lastErr = err
		backoff := time.Duration(1<<uint(attempt)) * time.Second
		slog.Warn("retryable error, backing off", "op", label, "attempt", attempt+1, "backoff", backoff, "error", err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return fmt.Errorf("failed after %d retries: %w", MaxRetries, lastErr)
}
