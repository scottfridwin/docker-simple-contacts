package db

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func fastRetryConfig() retryConfig {
	return retryConfig{initialBackoff: time.Millisecond, maxBackoff: 5 * time.Millisecond, maxWait: 100 * time.Millisecond}
}

func TestRetryUntilReadySucceedsAfterTransientFailures(t *testing.T) {
	attempts := 0
	err := retryUntilReady(context.Background(), slog.Default(), "test op", fastRetryConfig(), func() error {
		attempts++
		if attempts < 3 {
			return errors.New("not ready yet")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryUntilReady: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestRetryUntilReadyGivesUpAfterDeadline(t *testing.T) {
	err := retryUntilReady(context.Background(), slog.Default(), "test op", fastRetryConfig(), func() error {
		return errors.New("still not ready")
	})
	if err == nil {
		t.Fatal("expected error after exhausting retry budget")
	}
}

func TestRetryUntilReadyFailsFastOnPermanentError(t *testing.T) {
	attempts := 0
	sentinel := errors.New("dirty state")
	err := retryUntilReady(context.Background(), slog.Default(), "test op", fastRetryConfig(), func() error {
		attempts++
		return markPermanent(sentinel)
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want wrapped %v", err, sentinel)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (should not retry a permanent error)", attempts)
	}
}

func TestRetryUntilReadyStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := retryUntilReady(ctx, slog.Default(), "test op", retryConfig{initialBackoff: time.Second, maxBackoff: time.Second, maxWait: time.Minute}, func() error {
		attempts++
		cancel()
		return errors.New("not ready")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}
