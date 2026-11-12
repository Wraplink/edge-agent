package sniproxy

import (
	"bytes"
	"errors"
	"testing"
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
