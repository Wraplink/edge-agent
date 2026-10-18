package client

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

// HTTPStatusError preserves the upstream HTTP status for callers that need
// to distinguish transient registration failures from permanent errors.
type HTTPStatusError struct {
	StatusCode int
	Path       string
	BaseURL    string
	Detail     string
}

func (e *HTTPStatusError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("%s returned HTTP %d from %s", e.Path, e.StatusCode, e.BaseURL)
	}
	return fmt.Sprintf("%s returned HTTP %d from %s: %s", e.Path, e.StatusCode, e.BaseURL, e.Detail)
}

type Client struct {
	baseURL string
	token   string

	httpClient *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) Register(
	ctx context.Context,
	agentID string,
	region string,
	sniCapacity int,
	routeCapacity int,
) error {
	reqBody := RegisterRequest{
		AgentID: agentID,
		Region:  region,
		Capacity: Capacity{
			SNI:   sniCapacity,
			Route: routeCapacity,
		},
	}

	var response RegisterResponse
	if err := c.post(ctx, "/api/v1/agent/register", reqBody, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("dns-control registration did not confirm success: %s", c.baseURL)
	}
	return nil
}

func (c *Client) Poll(
	ctx context.Context,
	agentID string,
	sniAvailable int,
	routeAvailable int,
) (PollResponse, error) {
	reqBody := PollRequest{
		AgentID: agentID,
		Available: Capacity{
			SNI:   sniAvailable,
			Route: routeAvailable,
		},
	}

	var resp PollResponse
	if err := c.post(ctx, "/api/v1/agent/poll", reqBody, &resp); err != nil {
		return PollResponse{}, err
	}
	return resp, nil
}

func (c *Client) Heartbeat(
	ctx context.Context,
	agentID string,
	leaseIDs []string,
	entitlementRevision uint64,
) (HeartbeatResponse, error) {
	reqBody := HeartbeatRequest{
		AgentID:             agentID,
		LeaseIDs:            leaseIDs,
		EntitlementRevision: entitlementRevision,
	}

	var resp HeartbeatResponse
	if err := c.post(ctx, "/api/v1/agent/heartbeat", reqBody, &resp); err != nil {
		return HeartbeatResponse{}, err
	}
	return resp, nil
}

func (c *Client) Report(ctx context.Context, reqBody ReportRequest) error {
	return c.post(ctx, "/api/v1/agent/report", reqBody, nil)
}

func (c *Client) post(ctx context.Context, path string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+path,
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("create request %s: %w", path, err)
	}

	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Include the server's error text to distinguish dns-control errors
		// from reverse proxies or unrelated services on the same port.
		const maxErrorBytes = 2048
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes+1))
		if readErr != nil {
			return &HTTPStatusError{
				StatusCode: resp.StatusCode,
				Path:       path,
				BaseURL:    c.baseURL,
				Detail:    fmt.Sprintf("could not read response: %v", readErr),
			}
		}
		truncated := len(data) > maxErrorBytes
		if truncated {
			data = data[:maxErrorBytes]
		}
		detail := strings.TrimSpace(string(data))
		var serverErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &serverErr) == nil && serverErr.Error != "" {
			detail = serverErr.Error
		}
		detail = strings.ReplaceAll(strings.ReplaceAll(detail, "\r", " "), "\n", " ")
		if truncated {
			detail += " (truncated)"
		}
		return &HTTPStatusError{
			StatusCode: resp.StatusCode,
			Path:       path,
			BaseURL:    c.baseURL,
			Detail:     detail,
		}
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return nil
}

func (c *Client) authorize(req *http.Request) {
	if c.token == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
}
