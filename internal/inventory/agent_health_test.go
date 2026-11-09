package inventory

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentHealthClientRegistrationAndHeartbeat(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Network-Control-Key") != "secret" {
			t.Errorf("missing control key")
			w.WriteHeader(401)
			return
		}
		calls++
		switch calls {
		case 1:
			if r.URL.Path != "/api/v1/internal/network/agents/register" {
				t.Errorf("unexpected registration path %s", r.URL.Path)
			}
		case 2:
			if r.URL.Path != "/api/v1/internal/network/agents/heartbeat" {
				t.Errorf("unexpected heartbeat path %s", r.URL.Path)
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := AgentHealthClient{URL: server.URL, Key: "secret"}
	if err := client.Register(context.Background(), "edge-a", "eu-1", 10, 5); err != nil {
		t.Fatal(err)
	}
	if err := client.Heartbeat(context.Background(), "edge-a"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 requests, got %d", calls)
	}
}

func TestAgentHealthClientUnknownAgentOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	err := (AgentHealthClient{URL: server.URL, Key: "secret"}).Heartbeat(context.Background(), "missing")
	if !IsUnknownAgent(err) {
		t.Fatalf("404 must be recognized: %v", err)
	}
	if IsUnknownAgent(context.DeadlineExceeded) {
		t.Fatal("network errors are not unknown agents")
	}
}
