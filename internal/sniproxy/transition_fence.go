package sniproxy

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// transitionFence serializes an entitlement transition with measured usage
// checkpoints. A traffic-gated transition excludes concurrent forwarding writes.
// Backend acknowledgement and promotion are supplied by the caller.
type transitionFence struct {
	mu       sync.Mutex
	reporter *intervalReporter
	traffic  *sync.RWMutex
	blocked  *atomic.Bool
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
	if traffic == nil {
		return nil, fmt.Errorf("traffic fence is required")
	}
	f, err := newTransitionFence(reporter)
	if err != nil {
		return nil, err
	}
	f.traffic = traffic
	return f, nil
}

// newFailClosedTransition shares the blocked flag with every forwarding writer.
// A failed transition keeps future writes rejected until a successful retry.
func newFailClosedTransition(reporter *intervalReporter, traffic *sync.RWMutex, blocked *atomic.Bool) (*transitionFence, error) {
	if blocked == nil {
		return nil, fmt.Errorf("blocked flag is required")
	}
	f, err := newTrafficFencedTransition(reporter, traffic)
	if err != nil {
		return nil, err
	}
	f.blocked = blocked
	return f, nil
}

// transition checkpoints, waits for backend confirmation that old usage was
// applied, then promotes. Callbacks must check application, not mere delivery.
// Promotion retries must be idempotent. Traffic remains blocked on failure.
func (f *transitionFence) transition(at time.Time, awaitApplied, promote func() error) error {
	if awaitApplied == nil || promote == nil {
		return fmt.Errorf("usage acknowledgement and promotion callbacks are required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.traffic != nil {
		f.traffic.Lock()
		defer f.traffic.Unlock()
	}
	if f.blocked != nil {
		f.blocked.Store(true)
	}
	if err := f.reporter.flush(at); err != nil {
		return fmt.Errorf("persist transition checkpoint: %w", err)
	}
	if err := awaitApplied(); err != nil {
		return fmt.Errorf("confirm old subscription usage: %w", err)
	}
	if err := promote(); err != nil {
		return fmt.Errorf("promote reserved subscription: %w", err)
	}
	if f.blocked != nil {
		f.blocked.Store(false)
	}
	return nil
}
