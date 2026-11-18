package usage

import (
	"testing"
	"time"
)

func TestMeasuredBoundaryPartitionConservesExactBytes(t *testing.T) {
	start := time.Now().UTC()
	a := CounterCheckpoint{At: start, UploadedBytes: 100, DownloadedBytes: 200}
	b := CounterCheckpoint{At: start.Add(time.Second), UploadedBytes: 160, DownloadedBytes: 220}
	c := CounterCheckpoint{At: start.Add(2 * time.Second), UploadedBytes: 175, DownloadedBytes: 310}
	before, after, err := MeasuredBoundaryPartition("edge-1", "198.51.100.7", 10, 11, a, b, c)
	if err != nil {
		t.Fatal(err)
	}
	if before.UploadedBytes != 60 || before.DownloadedBytes != 20 || after.UploadedBytes != 15 || after.DownloadedBytes != 90 {
		t.Fatalf("incorrect measured deltas: before=%+v after=%+v", before, after)
	}
	if !before.EndedAt.Equal(after.StartedAt) || before.UploadedBytes+after.UploadedBytes != c.UploadedBytes-a.UploadedBytes ||
		before.DownloadedBytes+after.DownloadedBytes != c.DownloadedBytes-a.DownloadedBytes {
		t.Fatalf("boundary lost or duplicated bytes: before=%+v after=%+v", before, after)
	}
}

func TestMeasuredBoundaryPartitionRejectsCounterReset(t *testing.T) {
	start := time.Now().UTC()
	a := CounterCheckpoint{At: start, UploadedBytes: 100}
	b := CounterCheckpoint{At: start.Add(time.Second), UploadedBytes: 50}
	c := CounterCheckpoint{At: start.Add(2 * time.Second), UploadedBytes: 200}
	if _, _, err := MeasuredBoundaryPartition("edge-1", "198.51.100.7", 10, 11, a, b, c); err == nil {
		t.Fatal("counter reset must reject the partition")
	}
}

func TestMeasuredBoundaryPartitionRejectsRepeatedSequenceAndTimestamp(t *testing.T) {
	start := time.Now().UTC()
	a := CounterCheckpoint{At: start}
	b := CounterCheckpoint{At: start.Add(time.Second)}
	c := CounterCheckpoint{At: start.Add(2 * time.Second)}
	if _, _, err := MeasuredBoundaryPartition("edge-1", "198.51.100.7", 10, 10, a, b, c); err == nil {
		t.Fatal("same report sequence must be rejected")
	}
	if _, _, err := MeasuredBoundaryPartition("edge-1", "198.51.100.7", 10, 11, a, a, c); err == nil {
		t.Fatal("boundary outside interval must be rejected")
	}
}
