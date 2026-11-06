package usage

import (
	"testing"
	"time"
)

func TestSampleAndQueue(t *testing.T) {
	outbox, err := OpenOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewRouteSampler()
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	counters := []RouteCounter{{Key: "a", ClientIP: "192.0.2.1", Direction: "upload", Bytes: 100}}
	if _, err = s.SampleAndQueue(now, counters, outbox, "edge-01"); err != nil {
		t.Fatal(err)
	}
	counters[0].Bytes = 200
	deltas, err := s.SampleAndQueue(now.Add(time.Minute), counters, outbox, "edge-01")
	if err != nil || len(deltas) != 1 || deltas[0].UploadedBytes != 100 {
		t.Fatalf("delta: %+v %v", deltas, err)
	}
	pending, err := outbox.Pending()
	if err != nil || len(pending) != 1 || pending[0].UploadedBytes != 100 {
		t.Fatalf("pending: %+v %v", pending, err)
	}
	if _, err = s.SampleAndQueue(now.Add(2*time.Minute), counters, outbox, "edge-01"); err != nil {
		t.Fatal(err)
	}
	pending, err = outbox.Pending()
	if err != nil || len(pending) != 1 {
		t.Fatalf("duplicate: %+v %v", pending, err)
	}
}

func TestSampleAndQueueFailurePreservesBaseline(t *testing.T) {
	s := NewRouteSampler()
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	counters := []RouteCounter{{Key: "a", ClientIP: "192.0.2.1", Direction: "upload", Bytes: 100}}
	if _, err := s.SampleAndQueue(now, counters, nil, "edge-01"); err == nil {
		t.Fatal("expected missing outbox failure")
	}
	if !s.last.IsZero() {
		t.Fatal("failed collection changed baseline")
	}
}
