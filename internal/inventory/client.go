package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	URL  string
	Key  string
	HTTP *http.Client
}

func (c Client) Register(ctx context.Context, agentID string, addresses []string) error {
	body, err := json.Marshal(struct {
		AgentID   string   `json:"agentId"`
		PublicIPs []string `json:"publicIps"`
	}{agentID, addresses})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/api/v1/internal/network/route-agent-ips", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Network-Control-Key", c.Key)
	transport := c.HTTP
	if transport == nil {
		transport = &http.Client{Timeout: 8 * time.Second}
	}
	response, err := transport.Do(request)
	if err != nil {
		return fmt.Errorf("register route inventory: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("register route inventory: HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	return nil
}
