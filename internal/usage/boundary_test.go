package usage

import (
	"testing"
	"time"
)

func TestValidateBoundaryPartition(t *testing.T) {
	start := time.Now().UTC().Add(-time.Minute)
	boundary := start.Add(20 * time.Second)
	end := start.Add(time.Minute)
	original := Report{
		AgentID: "edge-1", Sequence: 1, ClientIP: "198.51.100.1",
		UploadedBytes: 120, DownloadedBytes: 250, StartedAt: start, EndedAt: end,
	}
	before := original
	before.Sequence = 2
	before.UploadedBytes = 30
	before.DownloadedBytes = 200
	before.EndedAt = boundary
	after := original
	after.Sequence = 3
	after.UploadedBytes = 90
	after.DownloadedBytes = 50
	after.StartedAt = boundary
	if err := ValidateBoundaryPartition(original, before, after, boundary); err != nil {
		t.Fatal(err)
	}
	after.UploadedBytes = 91
	if err := ValidateBoundaryPartition(original, before, after, boundary); err == nil {
		t.Fatal("incorrect byte allocation must fail")
	}
	after.UploadedBytes = 90
	after.StartedAt = boundary.Add(time.Second)
	if err := ValidateBoundaryPartition(original, before, after, boundary); err == nil {
		t.Fatal("interval gap must fail")
	}
	after.StartedAt = boundary
	after.Sequence = before.Sequence
	if err := ValidateBoundaryPartition(original, before, after, boundary); err == nil {
		t.Fatal("duplicate sequence must fail")
	}
	after.Sequence = 3
	before.Sequence = original.Sequence
	if err := ValidateBoundaryPartition(original, before, after, boundary); err == nil {
		t.Fatal("reusing original sequence must fail")
	}
	before.Sequence = 2
	after.Sequence = original.Sequence
	if err := ValidateBoundaryPartition(original, before, after, boundary); err == nil {
		t.Fatal("reusing original sequence for later interval must fail")
	}
}
