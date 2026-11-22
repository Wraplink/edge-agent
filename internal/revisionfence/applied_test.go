package revisionfence

import (
	"context"
	"errors"
	"testing"
)

func TestAppliedTrackerNeverConfirmsAcceptanceAlone(t *testing.T) {
	f := &Fence{}
	tracker := &AppliedTracker{}
	old := Operation{SubscriptionID: "sub", PublicIPID: "ip-a", Revision: 1, AttemptID: "attempt-a"}
	if err := f.Accept(old); err != nil {
		t.Fatal(err)
	}
	if tracker.IsApplied(old) {
		t.Fatal("accepted operation falsely reported applied")
	}
	if err := tracker.MarkApplied(context.Background(), f, old); err != nil {
		t.Fatal(err)
	}
	if !tracker.IsApplied(old) {
		t.Fatal("explicit confirmation not recorded")
	}
	next := Operation{SubscriptionID: "sub", PublicIPID: "ip-b", Revision: 2, AttemptID: "attempt-b"}
	if err := f.Accept(next); err != nil {
		t.Fatal(err)
	}
	if err := tracker.MarkApplied(context.Background(), f, old); !errors.Is(err, ErrNotApplied) {
		t.Fatalf("stale confirmation: %v", err)
	}
	if tracker.IsApplied(next) {
		t.Fatal("new revision falsely reported applied")
	}
	tracker.Reset()
	if tracker.IsApplied(old) {
		t.Fatal("confirmation survived reset")
	}
}

func TestAppliedTrackerRejectsCanceledContext(t *testing.T) {
	f := &Fence{}
	tracker := &AppliedTracker{}
	op := Operation{SubscriptionID: "sub", PublicIPID: "ip", Revision: 1, AttemptID: "attempt"}
	if err := f.Accept(op); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tracker.MarkApplied(ctx, f, op); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
	if tracker.IsApplied(op) {
		t.Fatal("canceled confirmation recorded")
	}
}

func TestCurrentAppliedRejectsSupersededRevision(t *testing.T) {
	f := &Fence{}
	tracker := &AppliedTracker{}
	old := Operation{SubscriptionID: "sub", PublicIPID: "ip-a", Revision: 1, AttemptID: "attempt-a"}
	next := Operation{SubscriptionID: "sub", PublicIPID: "ip-b", Revision: 2, AttemptID: "attempt-b"}
	if err := f.Accept(old); err != nil {
		t.Fatal(err)
	}
	if err := tracker.MarkApplied(context.Background(), f, old); err != nil {
		t.Fatal(err)
	}
	if !tracker.IsCurrentApplied(f, old) {
		t.Fatal("current applied revision not recognized")
	}
	if err := f.Accept(next); err != nil {
		t.Fatal(err)
	}
	if tracker.IsCurrentApplied(f, old) {
		t.Fatal("superseded revision reported current")
	}
	if tracker.IsCurrentApplied(f, next) {
		t.Fatal("unverified new revision reported applied")
	}
}
