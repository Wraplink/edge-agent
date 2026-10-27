package dataplane

import (
	"net"
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/client"
)

func TestEntitlementElements(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	grants := []client.AccessGrant{
		{ClientIP: "192.0.2.1", ExpiresAt: now.Add(30 * time.Second)},
		{ClientIP: "192.0.2.1", ExpiresAt: now.Add(90 * time.Second)},
		{ClientIP: "192.0.2.2", ExpiresAt: now.Add(-time.Second)},
		{ClientIP: "192.0.2.3", ExpiresAt: now.Add(time.Minute)},
	}
	elements, err := entitlementElements(grants, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(elements) != 2 {
		t.Fatalf("got %d elements, want 2", len(elements))
	}
	got := make(map[string]time.Duration)
	for _, element := range elements {
		got[net.IP(element.Key).String()] = element.Timeout
	}
	if got["192.0.2.1"] != 90*time.Second {
		t.Errorf("duplicate grant timeout = %v, want 90s", got["192.0.2.1"])
	}
	if got["192.0.2.3"] != time.Minute {
		t.Errorf("second client timeout = %v, want 1m", got["192.0.2.3"])
	}
	if _, exists := got["192.0.2.2"]; exists {
		t.Error("expired grant was retained")
	}
}

func TestEntitlementElementsRejectsInvalidIP(t *testing.T) {
	_, err := entitlementElements([]client.AccessGrant{{
		ClientIP: "not-an-ip",
		ExpiresAt: time.Now().Add(time.Hour),
	}}, time.Now())
	if err == nil {
		t.Fatal("expected invalid IPv4 error")
	}
}
