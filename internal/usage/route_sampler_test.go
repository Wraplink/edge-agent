package usage

import (
	"math"
	"testing"
	"time"
)

func TestRouteSamplerCountsBothDirectionsAndClients(t *testing.T) {
	s := NewRouteSampler()
	start := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	counters := []RouteCounter{
		{Key: "a-up", ClientIP: "192.0.2.1", Direction: "upload", Bytes: 100},
		{Key: "a-down", ClientIP: "192.0.2.1", Direction: "download", Bytes: 200},
		{Key: "b-up", ClientIP: "192.0.2.2", Direction: "upload", Bytes: 50},
	}
	if result, err := s.Sample(start, counters); err != nil || len(result) != 0 {
		t.Fatalf("baseline: %v %v", result, err)
	}
	counters[0].Bytes = 175
	counters[1].Bytes = 500
	counters[2].Bytes = 75
	result, err := s.Sample(start.Add(time.Minute), counters)
	if err != nil || len(result) != 2 {
		t.Fatalf("sample: %v %v", result, err)
	}
	if result[0].UploadedBytes != 75 || result[0].DownloadedBytes != 300 || result[1].UploadedBytes != 25 {
		t.Fatalf("wrong deltas: %+v", result)
	}
	result, err = s.Sample(start.Add(2*time.Minute), counters)
	if err != nil || len(result) != 0 {
		t.Fatalf("repeated sample: %v %v", result, err)
	}
}

func TestRouteSamplerFailsClosedOnResetAndMissingCounters(t *testing.T) {
	s := NewRouteSampler()
	now := time.Now()
	a := []RouteCounter{{Key: "counter", ClientIP: "192.0.2.1", Direction: "upload", Bytes: 100}}
	if _, err := s.Sample(now, a); err != nil {
		t.Fatal(err)
	}
	a[0].Bytes = 5
	if _, err := s.Sample(now.Add(time.Second), a); err == nil {
		t.Fatal("reset must fail closed")
	}
	if !s.last.Equal(now) {
		t.Fatal("reset advanced baseline")
	}
	if _, err := s.Sample(now.Add(time.Second), nil); err == nil {
		t.Fatal("missing counter must fail closed")
	}
	if !s.last.Equal(now) {
		t.Fatal("missing counter advanced baseline")
	}
	a[0].Bytes = 112
	result, err := s.Sample(now.Add(2*time.Second), a)
	if err != nil || len(result) != 1 || result[0].UploadedBytes != 12 {
		t.Fatalf("failed sample must not lose usage: %+v %v", result, err)
	}
}

func TestRouteSamplerRejectsCounterOverflowWithoutAdvancing(t *testing.T) {
	sampler := NewRouteSampler()
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	base := []RouteCounter{
		{Key: "one", ClientIP: "192.0.2.1", Direction: "upload", Bytes: 0},
		{Key: "two", ClientIP: "192.0.2.1", Direction: "upload", Bytes: 0},
	}
	if _, err := sampler.Sample(now, base); err != nil {
		t.Fatal(err)
	}
	huge := []RouteCounter{
		{Key: "one", ClientIP: "192.0.2.1", Direction: "upload", Bytes: math.MaxUint64},
		{Key: "two", ClientIP: "192.0.2.1", Direction: "upload", Bytes: 1},
	}
	if _, err := sampler.Sample(now.Add(time.Minute), huge); err == nil {
		t.Fatal("expected overflow rejection")
	}
	if !sampler.last.Equal(now) {
		t.Fatal("overflow advanced baseline")
	}
}

func TestRouteSamplerRejectsNewCounterWithExistingUsage(t *testing.T) {
	s := NewRouteSampler()
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	base := []RouteCounter{{Key: "existing", ClientIP: "192.0.2.1", Direction: "upload", Bytes: 10}}
	if _, err := s.Sample(now, base); err != nil {
		t.Fatal(err)
	}
	added := append(append([]RouteCounter{}, base...), RouteCounter{
		Key: "new", ClientIP: "192.0.2.2", Direction: "download", Bytes: 25,
	})
	if _, err := s.Sample(now.Add(time.Minute), added); err == nil {
		t.Fatal("new counter with preexisting bytes must fail closed")
	}
	if !s.last.Equal(now) {
		t.Fatal("failed sample advanced baseline")
	}
	added[1].Bytes = 0
	if _, err := s.Sample(now.Add(time.Minute), added); err != nil {
		t.Fatalf("new zeroed counter should be accepted: %v", err)
	}
}
