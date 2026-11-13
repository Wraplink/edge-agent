package sniproxy

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

type partialWriter struct{}

func (partialWriter) Write(p []byte) (int, error) {
	return len(p) / 2, errors.New("short write")
}

func TestMeteredWriterCountsAcceptedBytes(t *testing.T) {
	var destination bytes.Buffer
	var measured uint64
	writer := meteredWriter{destination: &destination, record: func(n uint64) { measured += n }}
	for _, chunk := range []string{"hello", "world"} {
		n, err := writer.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("write failed: n=%d err=%v", n, err)
		}
		if measured != uint64(destination.Len()) {
			t.Fatalf("counter lagged behind completed write: %d vs %d", measured, destination.Len())
		}
	}
}

func TestMeteredWriterCountsPartialWrite(t *testing.T) {
	var measured uint64
	writer := meteredWriter{destination: partialWriter{}, record: func(n uint64) { measured += n }}
	n, err := writer.Write([]byte("123456"))
	if err == nil || n != 3 || measured != 3 {
		t.Fatalf("expected only successful partial bytes counted: n=%d err=%v measured=%d", n, err, measured)
	}
}

type blockedWrite struct {
	entered chan struct{}
	release chan struct{}
}

func (w blockedWrite) Write(p []byte) (int, error) {
	close(w.entered)
	<-w.release
	return len(p), nil
}

// A checkpoint taking the exclusive gate must not observe forwarded bytes
// before their counter update has completed.
func TestMeteredWriterGateIncludesCounterUpdate(t *testing.T) {
	var gate sync.RWMutex
	entered := make(chan struct{})
	release := make(chan struct{})
	writeDone := make(chan struct{})
	var measured uint64
	writer := meteredWriter{destination: blockedWrite{entered, release}, record: func(n uint64) { measured += n }, gate: &gate}
	go func() {
		defer close(writeDone)
		_, _ = writer.Write([]byte("12345"))
	}()
	<-entered
	checkpointDone := make(chan uint64, 1)
	go func() {
		gate.Lock()
		checkpointDone <- measured
		gate.Unlock()
	}()
	select {
	case <-checkpointDone:
		t.Fatal("checkpoint passed while destination write was unfinished")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-writeDone
	select {
	case got := <-checkpointDone:
		if got != 5 {
			t.Fatalf("checkpoint measured %d, want 5", got)
		}
	case <-time.After(time.Second):
		t.Fatal("checkpoint did not complete")
	}
}
