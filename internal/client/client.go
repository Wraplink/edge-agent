package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

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

	return c.post(ctx, "/api/v1/agent/register", reqBody, nil)
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
) (HeartbeatResponse, error) {
	reqBody := HeartbeatRequest{
		AgentID:  agentID,
		LeaseIDs: leaseIDs,
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
		return fmt.Errorf("%s returned HTTP %d", path, resp.StatusCode)
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
