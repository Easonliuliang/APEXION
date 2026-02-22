package agent

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

const (
	maxRetries    = 3
	baseDelay     = 2 * time.Second
	maxDelay      = 30 * time.Second
	jitterPercent = 30 // ±30% jitter
)

// isRetryableError checks if an error is worth retrying (rate limit, server error, network).
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()

	// Rate limit (429)
	if strings.Contains(msg, "429") || strings.Contains(msg, "rate limit") || strings.Contains(msg, "rate_limit") {
		return true
	}
	// Anthropic overloaded (529)
	if strings.Contains(msg, "529") || strings.Contains(msg, "overloaded") {
		return true
	}
	// Server errors (500, 502, 503, 504)
	for _, code := range []string{"500", "502", "503", "504"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	// Network errors
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "temporary failure") {
		return true
	}
	// Context cancelled is NOT retryable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return false
}

// retryDelay returns the delay for attempt n (0-indexed) with jitter.
func retryDelay(attempt int) time.Duration {
	delay := baseDelay
	for range attempt {
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}
	// Add jitter: ±jitterPercent%
	jitter := time.Duration(rand.IntN(int(delay)*jitterPercent*2/100)) - time.Duration(int(delay)*jitterPercent/100)
	return delay + jitter
}

// sleepWithContext sleeps for d, but returns early if ctx is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// formatRetryMessage creates a user-friendly retry message.
func formatRetryMessage(attempt, maxAttempts int, delay time.Duration, err error) string {
	return fmt.Sprintf("Retrying (%d/%d) in %s... (%s)",
		attempt+1, maxAttempts, delay.Round(time.Millisecond), truncateError(err))
}

func truncateError(err error) string {
	s := err.Error()
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}
