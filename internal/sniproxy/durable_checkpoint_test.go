package sniproxy

import (
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/usage"
)

func TestDurableCheckpointEnqueuesExactDeltas(t *testing.T) {
	outbox, err := usage.OpenOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var meter sessionMeter
	start := time.Now().UTC()
	if err := meter.checkpointPersist("198.51.100.3", start, func(SessionUsage) error { return nil }); err != nil {
		t.Fatal(err)
	}
	meter.addUpload(125)
	meter.addDownload(300)
	if err := meter.durableCheckpoint("edge-1", "198.51.100.3", start.Add(time.Second), outbox); err != nil {
		t.Fatal(err)
	}
	meter.addUpload(25)
	if err := meter.durableCheckpoint("edge-1", "198.51.100.3", start.Add(2*time.Second), outbox); err != nil {
		t.Fatal(err)
	}
	pending, err := outbox.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].UploadedBytes != 125 || pending[0].DownloadedBytes != 300 || pending[1].UploadedBytes != 25 || pending[1].DownloadedBytes != 0 {
		t.Fatalf("unexpected persisted intervals: %+v", pending)
	}
	if pending[0].Sequence == pending[1].Sequence || !pending[1].StartedAt.Equal(pending[0].EndedAt) {
		t.Fatalf("overlapping intervals or repeated sequence: %+v", pending)
	}
}

func TestDurableCheckpointRejectsMissingOutboxWithoutAdvancing(t *testing.T) {
	var meter sessionMeter
	start := time.Now().UTC()
	if err := meter.checkpointPersist("198.51.100.3", start, func(SessionUsage) error { return nil }); err != nil {
		t.Fatal(err)
	}
	meter.addUpload(7)
	if err := meter.durableCheckpoint("edge-1", "198.51.100.3", start.Add(time.Second), nil); err == nil {
		t.Fatal("expected missing outbox error")
	}
	outbox, err := usage.OpenOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := meter.durableCheckpoint("edge-1", "198.51.100.3", start.Add(2*time.Second), outbox); err != nil {
		t.Fatal(err)
	}
	pending, err := outbox.Pending()
	if err != nil || len(pending) != 1 || pending[0].UploadedBytes != 7 || !pending[0].StartedAt.Equal(start) {
		t.Fatalf("checkpoint advanced on failure: %+v %v", pending, err)
	}
}
