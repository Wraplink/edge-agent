package sniproxy

import (
	"sync"
	"testing"
	"time"
)

func TestSessionMeterCountsBothDirections(t *testing.T) {
	var meter sessionMeter
	meter.addUpload(120)
	meter.addDownload(800)
	meter.addUpload(42)
	meter.addDownload(7)
	got := meter.snapshot("198.51.100.10")
	if got.ClientIP != "198.51.100.10" || got.UploadedBytes != 162 || got.DownloadedBytes != 807 {
		t.Fatalf("unexpected session usage: %+v", got)
	}
}

func TestSessionMeterEmptySession(t *testing.T) {
	var meter sessionMeter
	got := meter.snapshot("192.0.2.5")
	if got.UploadedBytes != 0 || got.DownloadedBytes != 0 {
		t.Fatalf("empty session must report zero bytes: %+v", got)
	}
}

func TestSessionMeterConcurrentSnapshots(t *testing.T) {
	var meter sessionMeter
	const iterations = 1000
	var writers sync.WaitGroup
	writers.Add(2)
	go func() {
		defer writers.Done()
		for i := 0; i < iterations; i++ {
			meter.addUpload(2)
		}
	}()
	go func() {
		defer writers.Done()
		for i := 0; i < iterations; i++ {
			meter.addDownload(3)
		}
	}()
	for i := 0; i < iterations; i++ {
		snapshot := meter.snapshot("198.51.100.1")
		if snapshot.UploadedBytes > 2*iterations || snapshot.DownloadedBytes > 3*iterations {
			t.Fatalf("invalid cumulative snapshot: %+v", snapshot)
		}
	}
	writers.Wait()
	final := meter.snapshot("198.51.100.1")
	if final.UploadedBytes != 2*iterations || final.DownloadedBytes != 3*iterations {
		t.Fatalf("lost concurrent forwarded bytes: %+v", final)
	}
}

func TestSessionMeterIncrementalCheckpoints(t *testing.T) {
	var meter sessionMeter
	start := time.Now().UTC()
	first, err := meter.checkpoint("198.51.100.1", start)
	if err != nil || first.UploadedBytes != 0 || first.DownloadedBytes != 0 {
		t.Fatalf("initial checkpoint: %+v %v", first, err)
	}
	meter.addUpload(120)
	meter.addDownload(250)
	middle := start.Add(time.Second)
	before, err := meter.checkpoint("198.51.100.1", middle)
	if err != nil || before.UploadedBytes != 120 || before.DownloadedBytes != 250 || !before.StartedAt.Equal(start) {
		t.Fatalf("first delta: %+v %v", before, err)
	}
	meter.addUpload(40)
	meter.addDownload(70)
	after, err := meter.checkpoint("198.51.100.1", middle.Add(time.Second))
	if err != nil || after.UploadedBytes != 40 || after.DownloadedBytes != 70 || !after.StartedAt.Equal(middle) {
		t.Fatalf("second delta: %+v %v", after, err)
	}
	if before.UploadedBytes+after.UploadedBytes != 160 || before.DownloadedBytes+after.DownloadedBytes != 320 {
		t.Fatal("checkpoint deltas do not conserve measured bytes")
	}
	if _, err := meter.checkpoint("198.51.100.1", middle); err == nil {
		t.Fatal("repeated or backwards checkpoint must fail")
	}
}
