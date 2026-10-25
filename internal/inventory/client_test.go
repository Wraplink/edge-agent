package inventory

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegister(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/internal/network/route-agent-ips" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("X-Network-Control-Key") != "secret" {
			t.Error("missing control key")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	c := Client{URL: server.URL, Key: "secret"}
	if err := c.Register(context.Background(), "edge-a", []string{"192.0.2.10"}); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterRejectsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()
	c := Client{URL: server.URL, Key: "wrong"}
	if err := c.Register(context.Background(), "edge-a", []string{"192.0.2.10"}); err == nil {
		t.Fatal("expected authentication error")
	}
}
