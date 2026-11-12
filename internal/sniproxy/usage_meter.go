package sniproxy

import "sync"

// sessionMeter counts bytes successfully forwarded in each direction.
// Upload and download may be updated concurrently by separate copy loops.
// A snapshot must read both counters without racing with those writers.
type sessionMeter struct {
	mu         sync.Mutex
	uploaded   uint64
	downloaded uint64
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
