package sniproxy

import (
	"fmt"
	"time"

	"github.com/wraplink/edge-agent/internal/usage"
)

// DurableCheckpoint records a measured SNI interval in the local usage outbox
// before advancing the session meter. The caller must initialize the meter
// checkpoint at session start and coordinate transition boundaries with the
// backend. This helper alone does not provide crash-atomic counter recovery.
func (m *sessionMeter) durableCheckpoint(agentID, clientIP string, at time.Time, outbox *usage.Outbox) error {
	if outbox == nil {
		return fmt.Errorf("usage outbox is required")
	}
	return m.checkpointPersist(clientIP, at, func(measurement SessionUsage) error {
		if measurement.StartedAt.IsZero() {
			return fmt.Errorf("session checkpoint start is required")
		}
		if measurement.UploadedBytes == 0 && measurement.DownloadedBytes == 0 {
			return nil
		}
		_, err := outbox.EnqueueNext(usage.Report{
			AgentID:         agentID,
			ClientIP:        clientIP,
			UploadedBytes:   measurement.UploadedBytes,
			DownloadedBytes: measurement.DownloadedBytes,
			StartedAt:       measurement.StartedAt,
			EndedAt:         measurement.EndedAt,
		})
		return err
	})
}
