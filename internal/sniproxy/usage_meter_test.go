package sniproxy

import "testing"

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
