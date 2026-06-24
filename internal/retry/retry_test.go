package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rinfra/rinfra/internal/retry"
)

func TestDo_RetriesTransientThenSucceeds(t *testing.T) {
	calls := 0
	err := retry.Do(context.Background(), 3, 0, retry.IsTransient, func() error {
		calls++
		if calls < 3 {
			return errors.New("rate limit exceeded")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDo_StopsOnPermanentError(t *testing.T) {
	calls := 0
	err := retry.Do(context.Background(), 5, 0, retry.IsTransient, func() error {
		calls++
		return errors.New("AccessDenied: invalid credentials")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("permanent error should not retry; got %d calls", calls)
	}
}

func TestDo_ExhaustsAttempts(t *testing.T) {
	calls := 0
	err := retry.Do(context.Background(), 3, 0, retry.IsTransient, func() error {
		calls++
		return errors.New("connection reset by peer")
	})
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if calls != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
}

func TestDo_ContextCancelAborts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	// base>0 so the wait hits the cancelled context between attempts.
	err := retry.Do(ctx, 3, time.Second, retry.IsTransient, func() error {
		calls++
		return errors.New("timeout")
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call before cancel abort, got %d", calls)
	}
}

func TestIsTransient(t *testing.T) {
	for _, s := range []string{"Throttling: rate exceeded", "503 Service Unavailable", "i/o timeout"} {
		if !retry.IsTransient(errors.New(s)) {
			t.Errorf("%q should be transient", s)
		}
	}
	if retry.IsTransient(errors.New("ValidationError: bad AMI id")) {
		t.Error("validation error should be permanent")
	}
	if retry.IsTransient(nil) {
		t.Error("nil is not transient")
	}
}
