package sniproxy

// sessionMeter counts bytes successfully forwarded in each direction.
// It is owned by one proxy handler and is not shared across goroutines.
type sessionMeter struct {
	uploaded   uint64
	downloaded uint64
}

func (m *sessionMeter) addUpload(n uint64) {
	m.uploaded += n
}

func (m *sessionMeter) addDownload(n uint64) {
	m.downloaded += n
}

func (m *sessionMeter) snapshot(ip string) SessionUsage {
	return SessionUsage{
		ClientIP:        ip,
		UploadedBytes:   m.uploaded,
		DownloadedBytes: m.downloaded,
	}
}
