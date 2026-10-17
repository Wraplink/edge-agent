package agent

import (
	"context"
	"testing"

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
