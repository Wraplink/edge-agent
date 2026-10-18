package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterReportsJSONErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/register" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer example-token" {
			t.Error("missing auth token")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"database temporarily unavailable"}`)
	}))
	defer server.Close()

	err := New(server.URL, "example-token").Register(context.Background(), "edge-01", "eu-1", 50, 10)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") ||
		!strings.Contains(err.Error(), "database temporarily unavailable") ||
		!strings.Contains(err.Error(), server.URL) {
		t.Fatalf("expected actionable HTTP 500 error, got %v", err)
	}
	if strings.Contains(err.Error(), "example-token") {
		t.Fatalf("error leaked auth token: %v", err)
	}
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected typed HTTP 500 for retry decisions, got %T: %v", err, err)
	}
}

func TestRegisterReportsPlainTextErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "upstream failed\ntry again")
	}))
	defer server.Close()

	err := New(server.URL, "example-token").Register(context.Background(), "edge-01", "eu-1", 50, 10)
	if err == nil || !strings.Contains(err.Error(), "upstream failed try again") {
		t.Fatalf("expected plain text response detail, got %v", err)
	}
}

func TestRegisterCapsErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, strings.Repeat("X", 4096))
	}))
	defer server.Close()

	err := New(server.URL, "example-token").Register(context.Background(), "edge-01", "eu-1", 50, 10)
	if err == nil || !strings.Contains(err.Error(), "(truncated)") || len(err.Error()) > 2300 {
		t.Fatalf("expected bounded response, got %v", err)
	}
}

func TestRegisterRequiresAcknowledgement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"ok":false}`)
	}))
	defer server.Close()
	err := New(server.URL, "test-token").Register(context.Background(), "edge-01", "eu-1", 50, 10)
	if err == nil || !strings.Contains(err.Error(), "did not confirm success") {
		t.Fatalf("a 200 without ok=true must not count as registered: %v", err)
	}
}
