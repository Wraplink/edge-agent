package usage

import (
	"fmt"
	"time"
)

// CounterCheckpoint is an actual cumulative measurement from a single
// connection or counter generation. The producer must take consistent
// snapshots; elapsed time must never be used to infer transferred bytes.
type CounterCheckpoint struct {
	At              time.Time
	UploadedBytes   uint64
	DownloadedBytes uint64
}

// MeasuredInterval returns an exact byte delta between two cumulative
// checkpoints. It fails closed on counter resets and invalid timestamps.
func MeasuredInterval(agentID, clientIP string, sequence uint64, start, end CounterCheckpoint) (Report, error) {
	if start.At.IsZero() || !end.At.After(start.At) {
		return Report{}, fmt.Errorf("counter checkpoint times must increase")
	}
	if end.UploadedBytes < start.UploadedBytes || end.DownloadedBytes < start.DownloadedBytes {
		return Report{}, fmt.Errorf("counter reset across measured interval")
	}
	report := Report{
		AgentID:         agentID,
		Sequence:        sequence,
		ClientIP:        clientIP,
		UploadedBytes:   end.UploadedBytes - start.UploadedBytes,
		DownloadedBytes: end.DownloadedBytes - start.DownloadedBytes,
		StartedAt:       start.At,
		EndedAt:         end.At,
	}
	if err := report.Validate(); err != nil {
		return Report{}, err
	}
	return report, nil
}
