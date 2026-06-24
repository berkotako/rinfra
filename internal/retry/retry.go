// Package retry provides a small exponential-backoff helper for the
// provisioning paths. Cloud and IaC operations fail transiently (rate limits,
// throttling, brief network blips, 5xx); a bounded retry turns those into
// success instead of a failed deploy, without masking real (permanent) errors —
// only errors classified as transient are retried.
package retry

import (
	"context"
	"strings"
	"time"
)

// Classifier reports whether an error is worth retrying.
type Classifier func(error) bool

// Do runs fn up to attempts times (attempts >= 1). Between tries it waits
// base, 2*base, 4*base, … but only retries while transient(err) is true; a
// non-transient error returns immediately. ctx cancellation aborts the wait and
// returns ctx.Err(). The last error is returned when attempts are exhausted.
func Do(ctx context.Context, attempts int, base time.Duration, transient Classifier, fn func() error) error {
	if attempts < 1 {
		attempts = 1
	}
	var err error
	delay := base
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if transient != nil && !transient(err) {
			return err // permanent — don't retry
		}
		if i == attempts-1 {
			break // last attempt; don't sleep
		}
		if delay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		} else if ctx.Err() != nil {
			return ctx.Err()
		}
		delay *= 2
	}
	return err
}

// transientMarkers are substrings that indicate a retryable, non-permanent
// failure across the cloud SDKs and the Pulumi/Terraform CLIs.
var transientMarkers = []string{
	"timeout", "timed out", "deadline exceeded",
	"connection reset", "connection refused", "broken pipe", "i/o timeout", "eof",
	"rate limit", "ratelimit", "throttl", "too many requests", "toomanyrequests",
	"temporarily", "try again", "temporary failure",
	"service unavailable", "serviceunavailable", "internalerror", "internal error",
	"500", "502", "503", "504",
}

// IsTransient is the default classifier: it matches common transient-failure
// markers (case-insensitive) in the error text. Conservative by design — an
// unrecognized error is treated as permanent so real failures surface fast.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, m := range transientMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}
