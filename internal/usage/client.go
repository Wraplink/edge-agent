package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Report is a single, immutable measurement interval. Sequence must remain
// stable across retries; the backend must deduplicate by agent and sequence.
type Report struct {
	AgentID         string    `json:"agentId"`
	Sequence        uint64    `json:"sequence"`
	ClientIP        string    `json:"clientIp"`
	UploadedBytes   uint64    `json:"uploadedBytes"`
	DownloadedBytes uint64    `json:"downloadedBytes"`
	StartedAt       time.Time `json:"startedAt"`
	EndedAt         time.Time `json:"endedAt"`
}

func (r Report) Validate() error {
	if strings.TrimSpace(r.AgentID) == "" || r.Sequence == 0 {
		return fmt.Errorf("agent ID and positive sequence are required")
	}
	ip := net.ParseIP(r.ClientIP)
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("client IP must be IPv4")
	}
	if r.StartedAt.IsZero() || !r.EndedAt.After(r.StartedAt) {
		return fmt.Errorf("invalid usage interval")
	}
	return nil
}

// Client submits already persisted usage records. It deliberately does not
// collect counters or manage retries; callers must use a durable outbox.
type Client struct {
	URL  string
	Key  string
	HTTP *http.Client
}

func (c Client) Submit(ctx context.Context, r Report) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if c.URL == "" || c.Key == "" {
		return fmt.Errorf("backend URL and control key are required")
	}
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/api/v1/internal/network/usage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Network-Control-Key", c.Key)
	req.Header.Set("Idempotency-Key", fmt.Sprintf("%s:%d", r.AgentID, r.Sequence))
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("submit usage: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("submit usage: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	return nil
}
