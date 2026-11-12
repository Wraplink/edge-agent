package sniproxy

import (
	"fmt"
	"sync"
	"time"
)

// sessionMeter counts bytes successfully forwarded in each direction.
// Upload and download may be updated concurrently by separate copy loops.
// A snapshot must read both counters without racing with those writers.
type sessionMeter struct {
	mu                 sync.Mutex
	uploaded           uint64
	downloaded         uint64
	checkpointAt       time.Time
	checkpointUpload   uint64
	checkpointDownload uint64
}

func (m *sessionMeter) addUpload(n uint64) {
	m.mu.Lock()
	m.uploaded += n
	m.mu.Unlock()
}

func (m *sessionMeter) addDownload(n uint64) {
	m.mu.Lock()
	m.downloaded += n
	m.mu.Unlock()
}

func (m *sessionMeter) snapshot(ip string) SessionUsage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return SessionUsage{
		ClientIP:        ip,
		UploadedBytes:   m.uploaded,
		DownloadedBytes: m.downloaded,
	}
}

// checkpoint measures the exact delta since the previous checkpoint.
// The caller must serialize report persistence with advancing this checkpoint;
// this in-memory operation alone is not crash-safe.
func (m *sessionMeter) checkpoint(ip string, at time.Time) (SessionUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if at.IsZero() {
		return SessionUsage{}, fmt.Errorf("checkpoint time is required")
	}
	if !m.checkpointAt.IsZero() && !at.After(m.checkpointAt) {
		return SessionUsage{}, fmt.Errorf("checkpoint time must increase")
	}
	if m.uploaded < m.checkpointUpload || m.downloaded < m.checkpointDownload {
		return SessionUsage{}, fmt.Errorf("session counters reset")
	}
	result := SessionUsage{
		StartedAt:       m.checkpointAt,
		EndedAt:         at,
		ClientIP:        ip,
		UploadedBytes:   m.uploaded - m.checkpointUpload,
		DownloadedBytes: m.downloaded - m.checkpointDownload,
	}
	m.checkpointAt = at
	m.checkpointUpload = m.uploaded
	m.checkpointDownload = m.downloaded
	return result, nil
}
