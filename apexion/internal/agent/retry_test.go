package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"rate limit 429", errors.New("status 429 too many requests"), true},
		{"rate_limit", errors.New("rate_limit_exceeded"), true},
		{"overloaded 529", errors.New("529 overloaded"), true},
		{"server 500", errors.New("internal server error 500"), true},
		{"bad gateway 502", errors.New("502 bad gateway"), true},
		{"service unavailable 503", errors.New("503 service unavailable"), true},
		{"gateway timeout 504", errors.New("504 gateway timeout"), true},
		{"connection refused", errors.New("dial tcp: connection refused"), true},
		{"timeout", errors.New("request timeout"), true},
		{"EOF", errors.New("unexpected EOF"), true},
		{"context canceled", context.Canceled, false},
		{"deadline exceeded", context.DeadlineExceeded, false},
		{"auth error", errors.New("401 unauthorized"), false},
		{"not found", errors.New("404 not found"), false},
		{"random error", errors.New("something went wrong"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableError(tt.err)
			if got != tt.expected {
				t.Errorf("isRetryableError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestRetryDelay(t *testing.T) {
	// Check exponential growth.
	d0 := retryDelay(0)
	d1 := retryDelay(1)
	d2 := retryDelay(2)

	// With jitter, we check rough ranges.
	if d0 < 1*time.Second || d0 > 4*time.Second {
		t.Errorf("attempt 0 delay %v out of expected range [1s, 4s]", d0)
	}
	if d1 < 2*time.Second || d1 > 8*time.Second {
		t.Errorf("attempt 1 delay %v out of expected range [2s, 8s]", d1)
	}
	if d2 < 4*time.Second || d2 > 16*time.Second {
		t.Errorf("attempt 2 delay %v out of expected range [4s, 16s]", d2)
	}

	// Check max cap.
	d10 := retryDelay(10)
	if d10 > maxDelay+maxDelay*jitterPercent/100 {
		t.Errorf("attempt 10 delay %v exceeds max %v", d10, maxDelay)
	}
}

func TestSleepWithContext_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	start := time.Now()
	err := sleepWithContext(ctx, 10*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected error from cancelled context")
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("sleep should have returned immediately, took %v", elapsed)
	}
}
