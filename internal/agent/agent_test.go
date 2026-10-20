package agent

import (
	"context"
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/allocator"
	"github.com/wraplink/edge-agent/internal/client"
)

type fakeSNIAllocator struct {
	released bool
}

func (f *fakeSNIAllocator) Allocate(context.Context, string) (allocator.Result, error) {
	return allocator.Result{}, nil
}

func (f *fakeSNIAllocator) Release(context.Context, string, allocator.Result) error {
	f.released = true
	return nil
}

type fakeDataplane struct {
	enforced bool
	grants   []client.AccessGrant
}

func (f *fakeDataplane) Apply(context.Context, client.Lease) error { return nil }
func (f *fakeDataplane) Remove(context.Context, string) error      { return nil }
func (f *fakeDataplane) ReplaceAccessGrants(_ context.Context, enforced bool, grants []client.AccessGrant) error {
	f.enforced = enforced
	f.grants = append([]client.AccessGrant(nil), grants...)
	return nil
}

func TestReleaseSNILeaseRemovesActiveState(t *testing.T) {
	sni := &fakeSNIAllocator{}
	a := &Agent{
		sniAllocator: sni,
		active: map[string]client.Lease{
			"lease-1": {
				ID:      "lease-1",
				Domain:  "game.example",
				Type:    TypeSNI,
				Address: "192.0.2.10",
				SNI:     "game.example",
			},
		},
	}
	if err := a.releaseLease(context.Background(), "lease-1"); err != nil {
		t.Fatalf("release lease: %v", err)
	}
	if !sni.released {
		t.Fatal("expected SNI allocator release")
	}
	if _, ok := a.active["lease-1"]; ok {
		t.Fatal("released lease must be removed from active state")
	}
}

func TestAvailableCapacityIsSeparatedByType(t *testing.T) {
	a := &Agent{
		sniCapacity:   3,
		routeCapacity: 2,
		active: map[string]client.Lease{
			"s1": {ID: "s1", Type: TypeSNI},
			"r1": {ID: "r1", Type: TypeRoute},
		},
	}
	sni, routeCapacity := a.availableCapacity()
	if sni != 2 || routeCapacity != 1 {
		t.Fatalf("expected sni=2 route=1, got sni=%d route=%d", sni, routeCapacity)
	}
}

func TestReplaceEntitlementsAppliesFullSnapshot(t *testing.T) {
	dataplane := &fakeDataplane{}
	a := &Agent{
		dataplane:    dataplane,
		active:       make(map[string]client.Lease),
		entitlements: make(map[string]client.AccessGrant),
	}
	grants := []client.AccessGrant{{EntitlementID: "ent-1", SubscriptionID: "sub-1", ClientIP: "203.0.113.10", ExpiresAt: time.Now().UTC().Add(time.Hour)}}
	if err := a.replaceEntitlements(context.Background(), true, 7, grants); err != nil {
		t.Fatalf("replace entitlements: %v", err)
	}
	if !dataplane.enforced || len(dataplane.grants) != 1 {
		t.Fatalf("unexpected dataplane snapshot: enforced=%t grants=%d", dataplane.enforced, len(dataplane.grants))
	}
	if !a.entitlementConfigured || !a.entitlementEnforced || a.entitlementRevision != 7 {
		t.Fatalf("unexpected entitlement state: configured=%t enforced=%t revision=%d", a.entitlementConfigured, a.entitlementEnforced, a.entitlementRevision)
	}
	if _, ok := a.entitlements["203.0.113.10"]; !ok {
		t.Fatal("expected grant to be indexed by client IP")
	}
}

func TestReplaceEntitlementsAcceptsEmptyRevocationSnapshot(t *testing.T) {
	dataplane := &fakeDataplane{}
	a := &Agent{
		dataplane:             dataplane,
		active:                make(map[string]client.Lease),
		entitlementConfigured: true,
		entitlementEnforced:   true,
		entitlementRevision:   4,
		entitlements: map[string]client.AccessGrant{
			"203.0.113.10": {ClientIP: "203.0.113.10"},
		},
	}
	if err := a.replaceEntitlements(context.Background(), true, 5, []client.AccessGrant{}); err != nil {
		t.Fatalf("replace entitlements: %v", err)
	}
	if len(dataplane.grants) != 0 || len(a.entitlements) != 0 {
		t.Fatal("empty entitlement snapshot must revoke every client")
	}
	if a.entitlementRevision != 5 {
		t.Fatalf("expected revision 5, got %d", a.entitlementRevision)
	}
}

func TestEntitlementRevisionRequiresResync(t *testing.T) {
	a := &Agent{
		entitlementConfigured: true,
		entitlementEnforced:   true,
		entitlementRevision:   8,
	}
	if a.entitlementSyncRequired(client.HeartbeatResponse{
		EntitlementEnforced: true,
		EntitlementRevision: 8,
	}) {
		t.Fatal("unchanged entitlement revision must not trigger resync")
	}
	if !a.entitlementSyncRequired(client.HeartbeatResponse{
		EntitlementEnforced: true,
		EntitlementRevision: 9,
	}) {
		t.Fatal("new entitlement revision must trigger resync")
	}
	if !a.entitlementSyncRequired(client.HeartbeatResponse{
		EntitlementEnforced: false,
		EntitlementRevision: 8,
	}) {
		t.Fatal("enforcement state change must trigger resync")
	}
}

func TestSNILeaseCannotActivateWithoutDataplane(t *testing.T) {
	a := &Agent{}
	err := a.processLease(context.Background(), client.Lease{
		ID:     "sni-lease-1",
		Type:   TypeSNI,
		Domain: "game.example",
	})
	if err == nil {
		t.Fatal("SNI lease must fail without a configured proxy dataplane")
	}
}
