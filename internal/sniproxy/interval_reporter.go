package sniproxy

import (
	"fmt"
	"sync"
	"time"
)

// intervalReporter emits non-overlapping measured intervals. It advances the
// meter cursor only after the callback confirms durable persistence. It is
// intentionally not enabled by default: the backend must coordinate plan
// boundaries before live reports can safely be billed.
type intervalReporter struct {
	mu      sync.Mutex
	meter   *sessionMeter
	ip      string
	persist func(SessionUsage) error
	closed  bool
}

func newIntervalReporter(meter *sessionMeter, ip string, started time.Time, persist func(SessionUsage) error) (*intervalReporter, error) {
	if meter == nil || persist == nil {
		return nil, fmt.Errorf("meter and persistence callback are required")
	}
	r := &intervalReporter{meter: meter, ip: ip, persist: persist}
	if err := meter.checkpointPersist(ip, started, func(SessionUsage) error { return nil }); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *intervalReporter) flush(at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("usage reporter is closed")
	}
	return r.meter.checkpointPersist(r.ip, at, func(measurement SessionUsage) error {
		if measurement.UploadedBytes == 0 && measurement.DownloadedBytes == 0 {
			return nil
		}
		return r.persist(measurement)
	})
}

func (r *intervalReporter) close(at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if err := r.meter.checkpointPersist(r.ip, at, func(measurement SessionUsage) error {
		if measurement.UploadedBytes == 0 && measurement.DownloadedBytes == 0 {
			return nil
		}
		return r.persist(measurement)
	}); err != nil {
		return err
	}
	r.closed = true
	return nil
}
