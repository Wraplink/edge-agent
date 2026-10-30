package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Outbox stores immutable reports as one file per sequence. Files are
// synchronized and atomically renamed before Enqueue returns.
type Outbox struct {
	mu  sync.Mutex
	dir string
}

func OpenOutbox(dir string) (*Outbox, error) {
	if dir == "" {
		return nil, fmt.Errorf("usage outbox directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Outbox{dir: dir}, nil
}

func (o *Outbox) filename(sequence uint64) string {
	return filepath.Join(o.dir, fmt.Sprintf("%020d.json", sequence))
}

func (o *Outbox) Enqueue(report Report) error {
	if err := report.Validate(); err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	path := o.filename(report.Sequence)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("usage sequence %d already exists", report.Sequence)
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(o.dir, ".usage-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return err
	}
	return syncDirectory(o.dir)
}

func (o *Outbox) Pending() ([]Report, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	entries, err := os.ReadDir(o.dir)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var reports []Report
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(o.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var report Report
		if err := json.Unmarshal(data, &report); err != nil {
			return nil, fmt.Errorf("decode %s: %w", entry.Name(), err)
		}
		if err := report.Validate(); err != nil {
			return nil, err
		}
		if entry.Name() != fmt.Sprintf("%020d.json", report.Sequence) {
			return nil, fmt.Errorf("usage report sequence mismatch: %s", entry.Name())
		}
		reports = append(reports, report)
	}
	return reports, nil
}

// Acknowledge deletes a report only after the backend confirms ingestion.
func (o *Outbox) Acknowledge(sequence uint64) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := os.Remove(o.filename(sequence)); err != nil {
		return err
	}
	return syncDirectory(o.dir)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
