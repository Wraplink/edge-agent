package sniproxy

import (
	"errors"
 "sync"
 "sync/atomic"
 "bytes"
	"testing"
	"time"
)

func TestTransitionFenceRequiresDurableCheckpoint(t *testing.T) {
	var meter sessionMeter
	start := time.Now().UTC()
	fail := true
	var persisted uint64
	reporter, err := newIntervalReporter(&meter, "198.51.100.8", start, func(m SessionUsage) error {
		if fail {
			return errors.New("outbox unavailable")
		}
		persisted += m.UploadedBytes
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fence, err := newTransitionFence(reporter)
	if err != nil {
		t.Fatal(err)
	}
	meter.addUpload(150)
	promotions := 0
	promote := func() error { promotions++; return nil }
	if err := fence.transition(start.Add(time.Second), func() error { return nil }, promote); err == nil {
		t.Fatal("expected checkpoint persistence failure")
	}
	if promotions != 0 {
		t.Fatal("promoted without durable usage checkpoint")
	}
	fail = false
	if err := fence.transition(start.Add(2*time.Second), func() error { return nil }, promote); err != nil {
		t.Fatal(err)
	}
	if promotions != 1 || persisted != 150 {
		t.Fatalf("unexpected accounting or promotion: promotions=%d persisted=%d", promotions, persisted)
	}
}

func TestTransitionFencePromotionFailureDoesNotDuplicateUsage(t *testing.T) {
	var meter sessionMeter
	start := time.Now().UTC()
	var persisted uint64
	reporter, err := newIntervalReporter(&meter, "198.51.100.8", start, func(m SessionUsage) error {
		persisted += m.DownloadedBytes
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fence, err := newTransitionFence(reporter)
	if err != nil {
		t.Fatal(err)
	}
	meter.addDownload(70)
	if err := fence.transition(start.Add(time.Second), func() error { return nil }, func() error { return errors.New("backend unavailable") }); err == nil {
		t.Fatal("expected promotion failure")
	}
	if err := fence.transition(start.Add(2*time.Second), func() error { return nil }, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if persisted != 70 {
		t.Fatalf("duplicate usage after promotion retry: %d", persisted)
	}
}

func TestTransitionFenceBlocksPromotionUntilUsageApplied(t *testing.T) {
	var meter sessionMeter
	start := time.Now().UTC()
	var persisted uint64
	reporter, err := newIntervalReporter(&meter, "198.51.100.8", start, func(m SessionUsage) error {
		persisted += m.UploadedBytes
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fence, err := newTransitionFence(reporter)
	if err != nil {
		t.Fatal(err)
	}
	meter.addUpload(99)
	promotions := 0
	promote := func() error { promotions++; return nil }
	if err := fence.transition(start.Add(time.Second), func() error { return errors.New("usage not applied") }, promote); err == nil {
		t.Fatal("expected backend acknowledgement failure")
	}
	if persisted != 99 || promotions != 0 {
		t.Fatalf("promoted before backend application: persisted=%d promotions=%d", persisted, promotions)
	}
	if err := fence.transition(start.Add(2*time.Second), func() error { return nil }, promote); err != nil {
		t.Fatal(err)
	}
	if persisted != 99 || promotions != 1 {
		t.Fatalf("retry duplicated usage or failed promotion: persisted=%d promotions=%d", persisted, promotions)
	}
	if err := fence.transition(start.Add(3*time.Second), nil, promote); err == nil {
		t.Fatal("missing acknowledgement must be rejected")
	}
}

func TestTrafficFencedTransitionBlocksForwardingUntilPromotion(t *testing.T) {
 var meter sessionMeter
 start := time.Now().UTC()
 reporter, err := newIntervalReporter(&meter, "198.51.100.8", start, func(SessionUsage) error { return nil })
 if err != nil { t.Fatal(err) }
 var gate sync.RWMutex
 fence, err := newTrafficFencedTransition(reporter, &gate)
 if err != nil { t.Fatal(err) }
 meter.addUpload(10)
 entered := make(chan struct{})
 release := make(chan struct{})
 finished := make(chan error, 1)
 go func() {
  finished <- fence.transition(start.Add(time.Second), func() error {
   close(entered)
   <-release
   return nil
  }, func() error { return nil })
 }()
 <-entered
 forwarded := make(chan struct{})
 go func() {
  gate.RLock()
  meter.addUpload(5)
  gate.RUnlock()
  close(forwarded)
 }()
 select {
 case <-forwarded: t.Fatal("traffic advanced before promotion completed")
 case <-time.After(20 * time.Millisecond):
 }
 close(release)
 if err := <-finished; err != nil { t.Fatal(err) }
 select {
 case <-forwarded:
 case <-time.After(time.Second): t.Fatal("forwarding did not resume after promotion")
 }
}

func TestFailClosedTransitionRejectsWritesUntilSuccessfulRetry(t *testing.T) {
 var meter sessionMeter
 start := time.Now().UTC()
 reporter, err := newIntervalReporter(&meter, "198.51.100.8", start, func(SessionUsage) error { return nil })
 if err != nil { t.Fatal(err) }
 var gate sync.RWMutex
 var blocked atomic.Bool
 fence, err := newFailClosedTransition(reporter, &gate, &blocked)
 if err != nil { t.Fatal(err) }
 var destination bytes.Buffer
 writer := meteredWriter{destination: &destination, record: meter.addUpload, gate: &gate, blocked: &blocked}
 if _, err := writer.Write([]byte("before")); err != nil { t.Fatal(err) }
 if err := fence.transition(start.Add(time.Second), func() error { return errors.New("not applied") }, func() error { return nil }); err == nil { t.Fatal("expected acknowledgement failure") }
 if !blocked.Load() { t.Fatal("failed transition did not block traffic") }
 if n, err := writer.Write([]byte("rejected")); n != 0 || err == nil { t.Fatalf("forwarded while blocked: n=%d err=%v", n, err) }
 if err := fence.transition(start.Add(2*time.Second), func() error { return nil }, func() error { return nil }); err != nil { t.Fatal(err) }
 if blocked.Load() { t.Fatal("successful retry left traffic blocked") }
 if _, err := writer.Write([]byte("after")); err != nil { t.Fatal(err) }
 if got := destination.String(); got != "beforeafter" { t.Fatalf("unexpected forwarded bytes %q", got) }
}
