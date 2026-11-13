package usage

import (
	"fmt"
	"time"
)

// MeasuredBoundaryPartition constructs two reports using three independently
// observed cumulative counter checkpoints. It does not interpolate traffic by
// time. The caller must obtain the middle checkpoint at the actual entitlement
// transition, before allowing traffic under the successor subscription.
func MeasuredBoundaryPartition(agentID, clientIP string, beforeSequence, afterSequence uint64, start, boundary, end CounterCheckpoint) (Report, Report, error) {
	if !boundary.At.After(start.At) || !end.At.After(boundary.At) {
		return Report{}, Report{}, fmt.Errorf("boundary checkpoint must be strictly inside the interval")
	}
	if beforeSequence == afterSequence {
		return Report{}, Report{}, fmt.Errorf("partition sequences must differ")
	}
	before, err := MeasuredInterval(agentID, clientIP, beforeSequence, start, boundary)
	if err != nil {
		return Report{}, Report{}, fmt.Errorf("measure pre-transition usage: %w", err)
	}
	after, err := MeasuredInterval(agentID, clientIP, afterSequence, boundary, end)
	if err != nil {
		return Report{}, Report{}, fmt.Errorf("measure post-transition usage: %w", err)
	}
	return before, after, nil
}
