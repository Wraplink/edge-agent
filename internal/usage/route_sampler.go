package usage

import (
    "fmt"
    "sort"
    "time"
)

// RouteCounter is a cumulative kernel byte counter for one routed client.
// The caller must only supply counters from FORWARD traffic belonging to
// WrapLink route leases, never SNI proxy or unrelated host traffic.
type RouteCounter struct {
    Key string
    ClientIP string
    Direction string // upload or download
    Bytes uint64
}

// RouteDelta is the interval measurement for a single routed client.
type RouteDelta struct {
    ClientIP string
    UploadedBytes uint64
    DownloadedBytes uint64
    StartedAt time.Time
    EndedAt time.Time
}

// RouteSampler converts cumulative per-client nftables counters to intervals.
// It is not crash durable: the caller must persist a checkpoint atomically with
// the generated reports before using it for production billing.
type RouteSampler struct {
    previous map[string]RouteCounter
    last time.Time
}

func NewRouteSampler() *RouteSampler {
    return &RouteSampler{previous: make(map[string]RouteCounter)}
}

// Sample initializes its baseline on the first call, so preexisting bytes
// cannot be charged twice. Removed/replaced counters are also rebaselined.
func (s *RouteSampler) Sample(now time.Time, counters []RouteCounter) ([]RouteDelta, error) {
    if now.IsZero() || (!s.last.IsZero() && !now.After(s.last)) {
        return nil, fmt.Errorf("route sample time must increase")
    }
    next := make(map[string]RouteCounter, len(counters))
    for _, c := range counters {
        if c.Key == "" || c.ClientIP == "" || (c.Direction != "upload" && c.Direction != "download") {
            return nil, fmt.Errorf("invalid route counter")
        }
        if _, ok := next[c.Key]; ok {
            return nil, fmt.Errorf("duplicate route counter key %q", c.Key)
        }
        next[c.Key] = c
    }
    if s.last.IsZero() {
        s.previous, s.last = next, now
        return nil, nil
    }
    totals := make(map[string]*RouteDelta)
    for key, c := range next {
        previous, ok := s.previous[key]
        if !ok || previous.ClientIP != c.ClientIP || previous.Direction != c.Direction || c.Bytes < previous.Bytes {
            continue // New, replaced or reset counter: establish new baseline.
        }
        delta := c.Bytes - previous.Bytes
        if delta == 0 { continue }
        entry := totals[c.ClientIP]
        if entry == nil {
            entry = &RouteDelta{ClientIP:c.ClientIP, StartedAt:s.last, EndedAt:now}
            totals[c.ClientIP] = entry
        }
        if c.Direction == "upload" { entry.UploadedBytes += delta } else { entry.DownloadedBytes += delta }
    }
    result := make([]RouteDelta, 0, len(totals))
    for _, item := range totals { result = append(result, *item) }
    sort.Slice(result, func(i,j int) bool { return result[i].ClientIP < result[j].ClientIP })
    s.previous, s.last = next, now
    return result, nil
}
