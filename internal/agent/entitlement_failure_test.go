package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/client"
)

type failingEntitlementObserver struct {
	calls int
}

func (f *failingEntitlementObserver) Replace(_ bool, _ []client.AccessGrant) error {
	f.calls++
	if f.calls == 1 {
		return errors.New("observer update failed")
	}
	return nil
}

func TestObserverFailureRevokesAndInvalidatesEntitlementRevision(t *testing.T) {
	dataplane := &fakeDataplane{}
	observer := &failingEntitlementObserver{}
	a := &Agent{
		dataplane:             dataplane,
		entitlementObserver:   observer,
		entitlementConfigured: true,
		entitlementEnforced:   true,
		entitlementRevision:   8,
		entitlements: map[string]client.AccessGrant{
			"203.0.113.10": {ClientIP: "203.0.113.10"},
		},
	}
	grants := []client.AccessGrant{{
		EntitlementID: "ent-2",
		ClientIP:      "203.0.113.11",
		ExpiresAt:     time.Now().UTC().Add(time.Hour),
	}}
	if err := a.replaceEntitlements(context.Background(), true, 9, grants); err == nil {
		t.Fatal("expected observer synchronization failure")
	}
	if !dataplane.enforced || len(dataplane.grants) != 0 {
		t.Fatalf("kernel access must be revoked: enforced=%t grants=%+v", dataplane.enforced, dataplane.grants)
	}
	if observer.calls != 2 {
		t.Fatalf("expected observer update followed by revocation, got %d calls", observer.calls)
	}
	if a.entitlementConfigured || a.entitlementRevision != 0 || len(a.entitlements) != 0 {
		t.Fatalf("failed revision must not be acknowledged: configured=%t revision=%d grants=%+v", a.entitlementConfigured, a.entitlementRevision, a.entitlements)
	}
	if !a.entitlementSyncRequired(client.HeartbeatResponse{EntitlementEnforced: true, EntitlementRevision: 9}) {
		t.Fatal("failed revision must be retried")
	}
}
