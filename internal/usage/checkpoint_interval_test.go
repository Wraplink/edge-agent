package usage

import (
	"testing"
	"time"
)

func TestMeasuredIntervalAndBoundaryPartition(t *testing.T) {
	start := time.Now().UTC().Add(-time.Minute)
	first := CounterCheckpoint{At: start, UploadedBytes: 100, DownloadedBytes: 500}
	middle := CounterCheckpoint{At: start.Add(20 * time.Second), UploadedBytes: 130, DownloadedBytes: 700}
	last := CounterCheckpoint{At: start.Add(time.Minute), UploadedBytes: 220, DownloadedBytes: 750}
	original, err := MeasuredInterval("edge-1", "198.51.100.1", 1, first, last)
	if err != nil {
		t.Fatal(err)
	}
	before, err := MeasuredInterval("edge-1", "198.51.100.1", 2, first, middle)
	if err != nil {
		t.Fatal(err)
	}
	after, err := MeasuredInterval("edge-1", "198.51.100.1", 3, middle, last)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBoundaryPartition(original, before, after, middle.At); err != nil {
		t.Fatal(err)
	}
	if before.UploadedBytes != 30 || before.DownloadedBytes != 200 ||
		after.UploadedBytes != 90 || after.DownloadedBytes != 50 {
		t.Fatalf("unexpected measured deltas: before=%+v after=%+v", before, after)
	}
}

func TestMeasuredIntervalRejectsResetAndInvalidTime(t *testing.T) {
	now := time.Now().UTC()
	start := CounterCheckpoint{At: now, UploadedBytes: 100, DownloadedBytes: 20}
	for _, end := range []CounterCheckpoint{
		{At: now.Add(time.Second), UploadedBytes: 99, DownloadedBytes: 20},
		{At: now.Add(time.Second), UploadedBytes: 100, DownloadedBytes: 19},
		{At: now, UploadedBytes: 101, DownloadedBytes: 21},
		{At: now.Add(-time.Second), UploadedBytes: 101, DownloadedBytes: 21},
	} {
		if _, err := MeasuredInterval("edge-1", "198.51.100.1", 1, start, end); err == nil {
			t.Fatalf("expected invalid checkpoint error for %+v", end)
		}
	}
}
