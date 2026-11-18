package sniproxy

import (
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/usage"
)

// This tests the complete interval-reporter -> outbox persistence boundary.
// Live scheduling and subscription transition synchronization are separate.
func TestIntervalReporterPersistsToOutbox(t *testing.T) {
	dir := t.TempDir()
	outbox, err := usage.OpenOutbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	var meter sessionMeter
	start := time.Now().UTC()
	ip := "198.51.100.9"
	reporter, err := newIntervalReporter(&meter, ip, start, func(measurement SessionUsage) error {
		_, err := outbox.EnqueueNext(usage.Report{
			AgentID:         "edge-1",
			ClientIP:        measurement.ClientIP,
			UploadedBytes:   measurement.UploadedBytes,
			DownloadedBytes: measurement.DownloadedBytes,
			StartedAt:       measurement.StartedAt,
			EndedAt:         measurement.EndedAt,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	meter.addUpload(120)
	if err := reporter.flush(start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	meter.addDownload(240)
	if err := reporter.close(start.Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reopened, err := usage.OpenOutbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := reopened.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].UploadedBytes != 120 || pending[1].DownloadedBytes != 240 ||
		!pending[0].EndedAt.Equal(pending[1].StartedAt) || pending[0].Sequence == pending[1].Sequence {
		t.Fatalf("incorrect durable intervals: %+v", pending)
	}
}
