package usage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSubmitUsesStableIdempotencyKey(t *testing.T) {
	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	report := Report{
		AgentID: "edge-eu-1", Sequence: 42, ClientIP: "198.51.100.10",
		UploadedBytes: 120, DownloadedBytes: 450,
		StartedAt: start, EndedAt: start.Add(time.Minute),
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/internal/network/usage" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Network-Control-Key") != "secret" {
			t.Error("missing authentication")
		}
		if r.Header.Get("Idempotency-Key") != "edge-eu-1:42" {
			t.Error("unstable idempotency key")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := Client{URL: server.URL, Key: "secret"}
	for i := 0; i < 2; i++ {
		if err := client.Submit(context.Background(), report); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 2 {
		t.Fatalf("expected two attempts, got %d", requests)
	}
}

func TestSubmitRejectsInvalidReportWithoutSending(t *testing.T) {
	start := time.Now()
	r := Report{AgentID: "edge-1", Sequence: 1, ClientIP: "invalid", StartedAt: start, EndedAt: start.Add(time.Minute)}
	if err := (Client{}).Submit(context.Background(), r); err == nil {
		t.Fatal("expected invalid IP error")
	}
}

func TestSubmitRejectsServerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	start := time.Now()
	r := Report{AgentID: "edge-1", Sequence: 1, ClientIP: "198.51.100.1", StartedAt: start, EndedAt: start.Add(time.Minute)}
	if err := (Client{URL: server.URL, Key: "secret"}).Submit(context.Background(), r); err == nil {
		t.Fatal("expected retryable server error")
	}
}
