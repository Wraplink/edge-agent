package sniproxy

import "io"

// meteredWriter records only bytes successfully accepted by the destination.
// It permits cumulative counters to advance during long-lived TCP streams,
// rather than waiting for io.Copy to finish.
type meteredWriter struct {
	destination io.Writer
	record      func(uint64)
}

func (w meteredWriter) Write(p []byte) (int, error) {
	n, err := w.destination.Write(p)
	if n > 0 && w.record != nil {
		w.record(uint64(n))
	}
	return n, err
}
