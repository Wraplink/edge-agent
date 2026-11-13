package sniproxy

import (
 "io"
 "fmt"
 "sync/atomic"
 "sync"
)

// meteredWriter records only bytes successfully accepted by the destination.
// When gate is set, it fences the destination write AND its counter update,
// so a checkpoint cannot fall between the two operations.
type meteredWriter struct {
 destination io.Writer
 record func(uint64)
 gate *sync.RWMutex
 blocked *atomic.Bool
}

func (w meteredWriter) Write(p []byte) (int, error) {
 if w.gate != nil {
  w.gate.RLock()
  defer w.gate.RUnlock()
 }
 if w.blocked != nil && w.blocked.Load() { return 0, fmt.Errorf("forwarding blocked pending entitlement transition") }
 n, err := w.destination.Write(p)
 if n > 0 && w.record != nil {
  w.record(uint64(n))
 }
 return n, err
}
