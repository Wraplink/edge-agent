package usage

import (
	"os"
	"path/filepath"
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
	if len(pending) != 1 || pending[0].UploadedBytes != 100 || pending[0].Sequence != 7 {
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
