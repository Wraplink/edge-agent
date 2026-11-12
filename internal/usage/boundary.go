package usage

import (
	"fmt"
	"time"
)

// ValidateBoundaryPartition checks independently measured reports split at a
// subscription transition. It deliberately does not estimate byte allocation
// from elapsed time: byte counts must come from actual counter measurements.
func ValidateBoundaryPartition(original Report, before Report, after Report, boundary time.Time) error {
	if err := original.Validate(); err != nil {
		return fmt.Errorf("original report: %w", err)
	}
	if err := before.Validate(); err != nil {
		return fmt.Errorf("before report: %w", err)
	}
	if err := after.Validate(); err != nil {
		return fmt.Errorf("after report: %w", err)
	}
	if !boundary.After(original.StartedAt) || !boundary.Before(original.EndedAt) {
		return fmt.Errorf("boundary must be inside original interval")
	}
	if before.AgentID != original.AgentID || after.AgentID != original.AgentID ||
		before.ClientIP != original.ClientIP || after.ClientIP != original.ClientIP {
		return fmt.Errorf("report identity mismatch")
	}
	if !before.StartedAt.Equal(original.StartedAt) || !before.EndedAt.Equal(boundary) ||
		!after.StartedAt.Equal(boundary) || !after.EndedAt.Equal(original.EndedAt) {
		return fmt.Errorf("reports must exactly partition the original interval")
	}
	if before.Sequence == after.Sequence {
		return fmt.Errorf("partition reports require distinct sequences")
	}
	if before.UploadedBytes > original.UploadedBytes ||
		after.UploadedBytes != original.UploadedBytes-before.UploadedBytes ||
		before.DownloadedBytes > original.DownloadedBytes ||
		after.DownloadedBytes != original.DownloadedBytes-before.DownloadedBytes {
		return fmt.Errorf("partition bytes do not match measured totals")
	}
	return nil
}
