package gcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	compute "google.golang.org/api/compute/v1"

	"github.com/rinfra/rinfra/internal/cloud"
)

// TestWaitOp_PollsUntilDone verifies waitOp keeps polling a not-yet-DONE
// operation and returns nil once it reaches DONE, and surfaces a terminal
// operation error.
func TestWaitOp_PollsUntilDone(t *testing.T) {
	ctx := context.Background()

	t.Run("polls then done", func(t *testing.T) {
		calls := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls == 1 {
				w.Write([]byte(`{"name":"op-1","status":"RUNNING"}`))
			} else {
				w.Write([]byte(`{"name":"op-1","status":"DONE"}`))
			}
		}))
		defer ts.Close()

		svc, err := (&provider{baseEndpoint: ts.URL}).computeService(ctx, cloud.Credentials{})
		if err != nil {
			t.Fatalf("computeService: %v", err)
		}
		// A global op (no Zone/Region) starting not-DONE → waitOp must poll.
		if err := waitOp(ctx, svc, "proj", &compute.Operation{Name: "op-1", Status: "PENDING"}); err != nil {
			t.Fatalf("waitOp: %v", err)
		}
		if calls < 2 {
			t.Errorf("expected waitOp to poll at least twice, got %d call(s)", calls)
		}
	})

	t.Run("surfaces operation error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{"name":"op-2","status":"DONE","error":{"errors":[{"message":"RESOURCE_IN_USE"}]}}`))
		}))
		defer ts.Close()
		svc, _ := (&provider{baseEndpoint: ts.URL}).computeService(ctx, cloud.Credentials{})
		err := waitOp(ctx, svc, "proj", &compute.Operation{Name: "op-2", Status: "RUNNING"})
		if err == nil {
			t.Fatal("expected an error from a failed operation")
		}
	})

	t.Run("nil and already-done are no-ops", func(t *testing.T) {
		svc, _ := (&provider{baseEndpoint: "http://127.0.0.1:0"}).computeService(ctx, cloud.Credentials{})
		if err := waitOp(ctx, svc, "proj", nil); err != nil {
			t.Errorf("nil op: %v", err)
		}
		if err := waitOp(ctx, svc, "proj", &compute.Operation{Name: "op", Status: "DONE"}); err != nil {
			t.Errorf("done op: %v", err)
		}
	})
}
