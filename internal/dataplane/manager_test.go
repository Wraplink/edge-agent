package dataplane

import (
	"context"
	"testing"

	"github.com/wraplink/edge-agent/internal/client"
)

func TestManagerApplyIgnoresNonRouteLease(t *testing.T) {
	manager := &Manager{active: make(map[string]client.Lease)}
	err := manager.Apply(context.Background(), client.Lease{Type: "sni"})
	if err != nil {
		t.Fatalf("Apply(sni) = %v, want nil", err)
	}
	if len(manager.active) != 0 {
		t.Fatalf("active leases = %d, want 0", len(manager.active))
	}
}

func TestManagerApplyRejectsInvalidRouteBeforeNFT(t *testing.T) {
	manager := &Manager{active: make(map[string]client.Lease)}
	tests := []struct {
		name  string
		lease client.Lease
	}{
		{name: "missing id", lease: client.Lease{Type: "route"}},
		{name: "missing address", lease: client.Lease{Type: "route", ID: "lease"}},
		{name: "missing destination", lease: client.Lease{Type: "route", ID: "lease", Address: "192.0.2.1"}},
		{name: "invalid protocol", lease: client.Lease{Type: "route", ID: "lease", Address: "192.0.2.1", DestinationIP: "198.51.100.1", Protocol: "icmp"}},
		{name: "invalid port range", lease: client.Lease{Type: "route", ID: "lease", Address: "192.0.2.1", DestinationIP: "198.51.100.1", Protocol: "tcp", DestinationPortStart: 443}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := manager.Apply(context.Background(), tt.lease); err == nil {
				t.Fatal("Apply() succeeded for invalid route")
			}
		})
	}
}

func TestManagerApplyIdenticalActiveLeaseSkipsNFT(t *testing.T) {
	lease := client.Lease{
		ID:            "lease",
		Type:          "route",
		Address:       "192.0.2.1",
		DestinationIP: "198.51.100.1",
		Protocol:      "tcp",
	}
	manager := &Manager{active: map[string]client.Lease{lease.ID: lease}}
	if err := manager.Apply(context.Background(), lease); err != nil {
		t.Fatalf("Apply(identical active lease) = %v", err)
	}
}

func TestManagerApplyChangedActiveLeaseReachesNFT(t *testing.T) {
	previous := client.Lease{
		ID:            "lease",
		Type:          "route",
		Address:       "192.0.2.1",
		DestinationIP: "198.51.100.1",
		Protocol:      "tcp",
	}
	changed := previous
	changed.DestinationIP = "198.51.100.2"
	manager := &Manager{active: map[string]client.Lease{previous.ID: previous}}
	defer func() {
		if recover() == nil {
			t.Fatal("changed active lease was skipped instead of reaching the NFT dataplane")
		}
	}()
	_ = manager.Apply(context.Background(), changed)
}
