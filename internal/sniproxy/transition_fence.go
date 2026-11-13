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
	traffic *sync.RWMutex
}

func newTransitionFence(reporter *intervalReporter) (*transitionFence, error) {
	if reporter == nil {
		return nil, fmt.Errorf("interval reporter is required")
	}
	return &transitionFence{reporter: reporter}, nil
}

// newTrafficFencedTransition requires the same lock used by meteredWriter.
// All forwarding writes must use that lock, including the initial ClientHello.
func newTrafficFencedTransition(reporter *intervalReporter, traffic *sync.RWMutex) (*transitionFence, error) {
 if traffic == nil { return nil, fmt.Errorf("traffic fence is required") }
 f, err := newTransitionFence(reporter)
 if err != nil { return nil, err }
 f.traffic = traffic
 return f, nil
}

// transition checkpoints, waits for Backend acknowledgement that all usage
// through the checkpoint was applied to the old subscription, then promotes.
// When constructed with a traffic gate, this method fences writes for the operation.\n// Callers must still fail closed on errors and prevent writes until a safe retry. The acknowledgement
// callback must verify actual Backend application, not merely outbox delivery.
// A failed acknowledgement or promotion must keep traffic fenced. Retried
// promotion must be idempotent.
func (f *transitionFence) transition(at time.Time, awaitApplied, promote func() error) error {
	if awaitApplied == nil || promote == nil {
		return fmt.Errorf("usage acknowledgement and promotion callbacks are required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
 if f.traffic != nil { f.traffic.Lock(); defer f.traffic.Unlock() }
	if err := f.reporter.flush(at); err != nil {
		return fmt.Errorf("persist transition checkpoint: %w", err)
	}
	if err := awaitApplied(); err != nil {
		return fmt.Errorf("confirm old subscription usage: %w", err)
	}
	if err := promote(); err != nil {
		return fmt.Errorf("promote reserved subscription: %w", err)
	}
	return nil
}
