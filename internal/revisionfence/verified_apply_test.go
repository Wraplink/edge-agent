package revisionfence

import (
	"context"
	"errors"
	"testing"
)

type fakeVerifiedDataplane struct {
	applyErr  error
	verifyErr error
	observed  bool
	applies   int
	verifies  int
}

func (d *fakeVerifiedDataplane) Apply(context.Context, Operation) error {
	d.applies++
	return d.applyErr
}
func (d *fakeVerifiedDataplane) Verify(context.Context, Operation) (bool, error) {
	d.verifies++
	return d.observed, d.verifyErr
}

func TestApplyVerifiedRequiresObservedDataplane(t *testing.T) {
	f := &Fence{}
	tracker := &AppliedTracker{}
	op := Operation{SubscriptionID: "sub", PublicIPID: "ip", Revision: 1, AttemptID: "attempt"}
	d := &fakeVerifiedDataplane{observed: false}
	if err := f.ApplyVerified(context.Background(), op, d, tracker); !errors.Is(err, ErrUnverifiedApplication) {
		t.Fatalf("unverified: %v", err)
	}
	if tracker.IsApplied(op) {
		t.Fatal("unverified revision reported applied")
	}
	d.observed = true
	if err := f.ApplyVerified(context.Background(), op, d, tracker); err != nil {
		t.Fatal(err)
	}
	if !tracker.IsApplied(op) {
		t.Fatal("verified revision not recorded")
	}
	if d.applies != 1 || d.verifies != 3 {
		t.Fatalf("applies=%d verifies=%d", d.applies, d.verifies)
	}
}

func TestApplyVerifiedFailsClosedOnApplyError(t *testing.T) {
	f := &Fence{}
	tracker := &AppliedTracker{}
	op := Operation{SubscriptionID: "sub", PublicIPID: "ip", Revision: 1, AttemptID: "attempt"}
	failure := errors.New("apply failed")
	d := &fakeVerifiedDataplane{applyErr: failure}
	if err := f.ApplyVerified(context.Background(), op, d, tracker); !errors.Is(err, failure) {
		t.Fatalf("apply error: %v", err)
	}
	if d.verifies != 1 || tracker.IsApplied(op) {
		t.Fatal("failed apply was verified or acknowledged")
	}
}

func TestApplyVerifiedRejectsStaleRevision(t *testing.T) {
	f := &Fence{}
	tracker := &AppliedTracker{}
	newer := Operation{SubscriptionID: "sub", PublicIPID: "ip-new", Revision: 3, AttemptID: "new"}
	old := Operation{SubscriptionID: "sub", PublicIPID: "ip-old", Revision: 2, AttemptID: "old"}
	d := &fakeVerifiedDataplane{observed: true}
	if err := f.ApplyVerified(context.Background(), newer, d, tracker); err != nil {
		t.Fatal(err)
	}
	if err := f.ApplyVerified(context.Background(), old, d, tracker); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale: %v", err)
	}
	if d.applies != 0 {
		t.Fatalf("stale operation reached dataplane: %d", d.applies)
	}
}

func TestApplyVerifiedSkipsAlreadyInstalledRevision(t *testing.T) {
	f := &Fence{}
	tracker := &AppliedTracker{}
	op := Operation{SubscriptionID: "sub", PublicIPID: "ip", Revision: 5, AttemptID: "attempt"}
	d := &fakeVerifiedDataplane{observed: true}
	if err := f.ApplyVerified(context.Background(), op, d, tracker); err != nil {
		t.Fatal(err)
	}
	if err := f.ApplyVerified(context.Background(), op, d, tracker); err != nil {
		t.Fatal(err)
	}
	if d.applies != 0 || d.verifies != 2 {
		t.Fatalf("replayed applied state: applies=%d verifies=%d", d.applies, d.verifies)
	}
	if !tracker.IsCurrentApplied(f, op) {
		t.Fatal("verified revision not current")
	}
}

type cancelAfterVerifyDataplane struct {
	cancel context.CancelFunc
	checks int
}

func (d *cancelAfterVerifyDataplane) Apply(context.Context, Operation) error { return nil }
func (d *cancelAfterVerifyDataplane) Verify(context.Context, Operation) (bool, error) {
	d.checks++
	if d.checks == 1 {
		return false, nil
	}
	d.cancel()
	return true, nil
}

func TestApplyVerifiedRecordsObservationAfterCancellation(t *testing.T) {
	f := &Fence{}
	tracker := &AppliedTracker{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &cancelAfterVerifyDataplane{cancel: cancel}
	op := Operation{SubscriptionID: "sub", PublicIPID: "ip", Revision: 7, AttemptID: "attempt"}
	if err := f.ApplyVerified(ctx, op, d, tracker); err != nil {
		t.Fatalf("verified operation: %v", err)
	}
	if !tracker.IsCurrentApplied(f, op) {
		t.Fatal("lost confirmed local observation after cancellation")
	}
}

func TestVerifyCurrentAppliedRequiresFreshObservation(t *testing.T) {
	f := &Fence{}
	tracker := &AppliedTracker{}
	op := Operation{SubscriptionID: "sub", PublicIPID: "ip", Revision: 1, AttemptID: "attempt"}
	d := &fakeVerifiedDataplane{observed: true}
	if err := f.ApplyVerified(context.Background(), op, d, tracker); err != nil {
		t.Fatal(err)
	}
	d.observed = false
	ok, err := f.VerifyCurrentApplied(context.Background(), op, d, tracker)
	if err != nil || ok {
		t.Fatalf("removed dataplane still confirmed: ok=%v err=%v", ok, err)
	}
	d.observed = true
	ok, err = f.VerifyCurrentApplied(context.Background(), op, d, tracker)
	if err != nil || !ok {
		t.Fatalf("live dataplane not confirmed: ok=%v err=%v", ok, err)
	}
	next := Operation{SubscriptionID: "sub", PublicIPID: "next", Revision: 2, AttemptID: "next"}
	if err := f.Accept(next); err != nil {
		t.Fatal(err)
	}
	before := d.verifies
	ok, err = f.VerifyCurrentApplied(context.Background(), op, d, tracker)
	if err != nil || ok || d.verifies != before {
		t.Fatalf("superseded operation reached dataplane: ok=%v err=%v", ok, err)
	}
	ok, err = f.VerifyCurrentApplied(context.Background(), next, d, tracker)
	if err != nil || ok || d.verifies != before {
		t.Fatalf("unverified new revision confirmed: ok=%v err=%v", ok, err)
	}
}

func TestVerifyCurrentAppliedFailsClosedOnObservationError(t *testing.T) {
	f := &Fence{}
	tracker := &AppliedTracker{}
	op := Operation{SubscriptionID: "sub", PublicIPID: "ip", Revision: 1, AttemptID: "attempt"}
	d := &fakeVerifiedDataplane{observed: true}
	if err := f.ApplyVerified(context.Background(), op, d, tracker); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("dataplane inspection unavailable")
	d.verifyErr = failure
	ok, err := f.VerifyCurrentApplied(context.Background(), op, d, tracker)
	if ok || !errors.Is(err, failure) {
		t.Fatalf("observation error accepted: ok=%v err=%v", ok, err)
	}
}
