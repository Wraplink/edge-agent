package usage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOutboxSurvivesReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "usage")
	outbox, err := OpenOutbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC()
	report := Report{AgentID: "edge-1", Sequence: 7, ClientIP: "198.51.100.2", UploadedBytes: 100, DownloadedBytes: 200, StartedAt: start, EndedAt: start.Add(time.Minute)}
	if err := outbox.Enqueue(report); err != nil {
		t.Fatal(err)
	}
	if err := outbox.Enqueue(report); err == nil {
		t.Fatal("duplicate sequence must fail")
	}
	reopened, err := OpenOutbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := reopened.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].UploadedBytes != 100 || pending[0].Sequence != 7 || !pending[0].StartedAt.Equal(start) || !pending[0].EndedAt.Equal(start.Add(time.Minute)) {
		t.Fatalf("unexpected pending reports: %+v", pending)
	}
	if err := reopened.Acknowledge(7); err != nil {
		t.Fatal(err)
	}
	pending, err = reopened.Pending()
	if err != nil || len(pending) != 0 {
		t.Fatalf("expected empty outbox: %+v, %v", pending, err)
	}
}

func TestOutboxRejectsCorruptedReport(t *testing.T) {
	dir := t.TempDir()
	outbox, err := OpenOutbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "00000000000000000001.json"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.Pending(); err == nil {
		t.Fatal("corrupt report must not be silently discarded")
	}
}

func TestSequenceReservationSurvivesRestartAndAcknowledgement(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenOutbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := first.ReserveSequence()
	if err != nil || a != 1 {
		t.Fatalf("first sequence = %d, %v", a, err)
	}
	b, err := first.ReserveSequence()
	if err != nil || b != 2 {
		t.Fatalf("second sequence = %d, %v", b, err)
	}
	restarted, err := OpenOutbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	next, err := restarted.ReserveSequence()
	if err != nil || next != 3 {
		t.Fatalf("sequence after restart = %d, %v", next, err)
	}
}

func TestEnqueueNextPersistsSequenceAndReport(t *testing.T) {
	dir := t.TempDir()
	outbox, err := OpenOutbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC()
	report := Report{AgentID: "edge-1", ClientIP: "198.51.100.2", UploadedBytes: 42, StartedAt: start, EndedAt: start.Add(time.Second)}
	sequence, err := outbox.EnqueueNext(report)
	if err != nil || sequence != 1 {
		t.Fatalf("first enqueue: sequence=%d err=%v", sequence, err)
	}
	reopened, err := OpenOutbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := reopened.Pending()
	if err != nil || len(pending) != 1 || pending[0].Sequence != sequence || pending[0].UploadedBytes != 42 {
		t.Fatalf("durable report: %+v err=%v", pending, err)
	}
	next, err := reopened.EnqueueNext(report)
	if err != nil || next != 2 {
		t.Fatalf("next sequence after restart: %d %v", next, err)
	}
}

func TestEnqueueNextConcurrentSequences(t *testing.T) {
	outbox, err := OpenOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const count = 24
	start := time.Now().UTC()
	report := Report{AgentID: "edge-1", ClientIP: "198.51.100.2", UploadedBytes: 1, StartedAt: start, EndedAt: start.Add(time.Second)}
	var wg sync.WaitGroup
	sequences := make(chan uint64, count)
	errors := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sequence, err := outbox.EnqueueNext(report)
			if err != nil {
				errors <- err
				return
			}
			sequences <- sequence
		}()
	}
	wg.Wait()
	close(sequences)
	close(errors)
	for err := range errors {
		t.Errorf("concurrent enqueue: %v", err)
	}
	seen := make(map[uint64]bool)
	for sequence := range sequences {
		if seen[sequence] {
			t.Fatalf("duplicate sequence %d", sequence)
		}
		seen[sequence] = true
	}
	pending, err := outbox.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != count || len(pending) != count {
		t.Fatalf("got %d sequences and %d durable reports, want %d", len(seen), len(pending), count)
	}
	for i := uint64(1); i <= count; i++ {
		if !seen[i] {
			t.Fatalf("missing sequence %d", i)
		}
	}
}
