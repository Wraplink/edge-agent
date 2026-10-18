package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/client"
)

func TestRegistrationRetriesTransientFiveHundred(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprint(w, `{"error":"dns-control warming up"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"agent":{"id":"edge-01"}}`)
	}))
	defer server.Close()

	a := &Agent{
		id: "edge-01", region: "eu-1",
		sniCapacity: 10, routeCapacity: 2,
		dnsClient: client.New(server.URL, "example-token"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.registerWithRetry(ctx, time.Millisecond, 5*time.Millisecond); err != nil {
		t.Fatalf("registration should recover: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("registration should retry twice before success: attempts=%d", got)
	}
}

func TestRegistrationFailsFastForInvalidCredentials(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"unauthorized"}`)
	}))
	defer server.Close()

	a := &Agent{dnsClient: client.New(server.URL, "wrong-token")}
	err := a.registerWithRetry(context.Background(), time.Millisecond, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("expected actionable authorization error, got %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("permanent error must not be retried: attempts=%d", got)
	}
}

func TestRegistrationRetriesUntilCanceledWithoutPolling(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error":"temporary upstream failure"}`)
	}))
	defer server.Close()

	a := &Agent{dnsClient: client.New(server.URL, "example-token")}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	err := a.registerWithRetry(ctx, time.Millisecond, 5*time.Millisecond)
	if err == nil || ctx.Err() == nil {
		t.Fatalf("expected clean cancellation, got %v", err)
	}
	if got := attempts.Load(); got < 2 {
		t.Fatalf("expected repeated registration attempts while waiting: %d", got)
	}
}

func TestRegistrationRetriesUnavailableHTTPServer(t *testing.T) {
	// Start on an unused TCP address and close the listener so the first
	// request gets connection refused. A real server can come online later.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	url := server.URL
	server.Close()

	a := &Agent{dnsClient: client.New(url, "example-token")}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	err := a.registerWithRetry(ctx, time.Millisecond, 5*time.Millisecond)
	if err == nil || ctx.Err() == nil {
		t.Fatalf("unavailable server should retry until context cancellation: %v", err)
	}
}
