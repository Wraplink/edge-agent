package sniproxy

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestIntervalReporterRetryAndFinalClose(t *testing.T) {
	var meter sessionMeter
	start := time.Now().UTC()
	var saved []SessionUsage
	fail := true
	reporter, err := newIntervalReporter(&meter, "198.51.100.8", start, func(measurement SessionUsage) error {
		if fail {
			return errors.New("disk full")
		}
		saved = append(saved, measurement)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	meter.addUpload(100)
	if err := reporter.flush(start.Add(time.Second)); err == nil {
		t.Fatal("expected persistence failure")
	}
	meter.addUpload(25)
	fail = false
	if err := reporter.flush(start.Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	meter.addDownload(80)
	if err := reporter.close(start.Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := reporter.close(start.Add(4 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 || saved[0].UploadedBytes != 125 || saved[1].DownloadedBytes != 80 || !saved[0].StartedAt.Equal(start) || !saved[1].StartedAt.Equal(saved[0].EndedAt) {
		t.Fatalf("incorrect persisted intervals: %+v", saved)
	}
	if err := reporter.flush(start.Add(5 * time.Second)); err == nil {
		t.Fatal("closed reporter accepted a new interval")
	}
}

func TestIntervalReporterConcurrentFlushes(t *testing.T) {
	var meter sessionMeter
	start := time.Now().UTC()
	var mu sync.Mutex
	var saved []SessionUsage
	reporter, err := newIntervalReporter(&meter, "198.51.100.8", start, func(measurement SessionUsage) error {
		mu.Lock()
		saved = append(saved, measurement)
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	meter.addUpload(200)
	var wg sync.WaitGroup
	for i := 1; i <= 12; i++ {
		at := start.Add(time.Duration(i) * time.Second)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reporter.flush(at) // Older concurrent timestamps are intentionally rejected.
		}()
	}
	wg.Wait()
	if err := reporter.close(start.Add(13 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var total uint64
	for _, interval := range saved {
		total += interval.UploadedBytes
	}
	if total != 200 {
		t.Fatalf("expected 200 bytes exactly once, got %d across %+v", total, saved)
	}
}
