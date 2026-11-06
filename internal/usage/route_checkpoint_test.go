package usage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRouteSamplerCheckpointRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "route-state.json")
	s, err := LoadRouteSampler(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	base := []RouteCounter{{Key: "lease:client:up", ClientIP: "192.0.2.5", Direction: "upload", Bytes: 100}}
	if _, err = s.Sample(now, base); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveCheckpoint(path); err != nil {
		t.Fatal(err)
	}
	restored, err := LoadRouteSampler(path)
	if err != nil {
		t.Fatal(err)
	}
	base[0].Bytes = 145
	delta, err := restored.Sample(now.Add(time.Minute), base)
	if err != nil || len(delta) != 1 || delta[0].UploadedBytes != 45 {
		t.Fatalf("restored delta: %+v %v", delta, err)
	}
}

func TestRouteSamplerCheckpointRejectsCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	s, err := LoadRouteSampler(path)
	if err != nil || s == nil {
		t.Fatalf("missing checkpoint: %v", err)
	}
	if err = s.SaveCheckpoint(path); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadRouteSampler(path); err == nil {
		t.Fatal("empty baseline must not be accepted as a persisted checkpoint")
	}
}
