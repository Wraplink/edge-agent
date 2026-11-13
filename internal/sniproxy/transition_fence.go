package sniproxy

import (
	"fmt"
	"sync"
	"time"
)

// transitionFence serializes an entitlement transition with measured usage
// checkpoints. The caller MUST hold the same fence across any traffic writes
// that may be charged to this session. This primitive alone does not pause
// live network I/O or perform the backend subscription promotion.
type transitionFence struct {
	mu       sync.Mutex
	reporter *intervalReporter
}

func newTransitionFence(reporter *intervalReporter) (*transitionFence, error) {
	if reporter == nil {
		return nil, fmt.Errorf("interval reporter is required")
	}
	return &transitionFence{reporter: reporter}, nil
}

// transition checkpoints and durably persists all bytes before invoking
// promote. A persistence failure prevents promotion. A promotion failure
// leaves the checkpoint committed; retries must use a new checkpoint and an
// idempotent backend promotion operation.
func (f *transitionFence) transition(at time.Time, promote func() error) error {
	if promote == nil {
		return fmt.Errorf("promotion callback is required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.reporter.flush(at); err != nil {
		return fmt.Errorf("persist transition checkpoint: %w", err)
	}
	if err := promote(); err != nil {
		return fmt.Errorf("promote reserved subscription: %w", err)
	}
	return nil
}
