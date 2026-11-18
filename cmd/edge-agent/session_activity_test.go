package main

import (
	"testing"
	"time"
)

func TestMaxSessionActivity(t *testing.T) {
	connected := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name           string
		observed, want time.Time
	}{
		{"normal", connected.Add(time.Second), connected.Add(time.Second)},
		{"same instant", connected, connected},
		{"clock moved backwards", connected.Add(-time.Minute), connected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := maxSessionActivity(tc.observed, connected); !got.Equal(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
