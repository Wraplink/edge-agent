package sessioncontrol

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Terminator closes one agent-local connection; it must not revoke other sessions.
type Terminator interface {
	DisconnectSession(string) bool
}

type Command struct {
	CommandID      string `json:"commandId"`
	AgentSessionID string `json:"agentSessionId"`
}

type commandResponse struct {
	Commands []Command `json:"commands"`
}

func validCommandID(value string) bool {
	if len(value) != 36 {
		return false
	}
	var digits [32]byte
	offset := 0
	for i := 0; i < len(value); i++ {
		switch i {
		case 8, 13, 18, 23:
			if value[i] != '-' {
				return false
			}
		default:
			digits[offset] = value[i]
			offset++
		}
	}
	var decoded [16]byte
	if _, err := hex.Decode(decoded[:], digits[:]); err != nil {
		return false
	}
	for _, b := range decoded {
		if b != 0 {
			return true
		}
	}
	return false
}

// Client polls a trusted backend endpoint using a separately provisioned token.
// It is not enabled automatically: agent registration and credentials must first
// be bound to a verified backend identity.
type Client struct {
	BaseURL    string
	AgentID    string
	Token      string
	HTTP       *http.Client
	Terminator Terminator
}

func (c *Client) request(ctx context.Context, method, path, body string) (*http.Response, error) {
	if c.Token == "" || c.AgentID == "" || c.Terminator == nil {
		return nil, errors.New("session control is not configured")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("session control requires an HTTPS backend URL")
	}
	endpoint := strings.TrimRight(u.String(), "/") + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-Network-Agent-ID", c.AgentID)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	// Never follow redirects with agent credentials.
	guarded := *client
	guarded.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return guarded.Do(req)
}

// PollOnce processes only instructions returned by the authenticated backend.
// An absent local session is acknowledged as failed, never as a success.
func (c *Client) PollOnce(ctx context.Context) error {
	resp, err := c.request(ctx, http.MethodGet, "/api/v1/internal/network/sessions/commands", "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("poll commands: HTTP %d", resp.StatusCode)
	}
	var payload commandResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing command response data")
	}
	if len(payload.Commands) > 100 {
		return errors.New("too many disconnect commands")
	}
	// Validate the complete batch before terminating any connections.
	seen := make(map[string]struct{}, len(payload.Commands))
	seenSessions := make(map[string]struct{}, len(payload.Commands))
	for _, command := range payload.Commands {
		if !validCommandID(command.CommandID) || command.AgentSessionID == "" {
			return errors.New("invalid disconnect instruction")
		}
		if _, duplicate := seen[command.CommandID]; duplicate {
			return errors.New("duplicate disconnect command")
		}
		seen[command.CommandID] = struct{}{}
		if _, duplicate := seenSessions[command.AgentSessionID]; duplicate {
			return errors.New("duplicate target session in command batch")
		}
		seenSessions[command.AgentSessionID] = struct{}{}
	}
	for _, command := range payload.Commands {
		status := "failed"
		errorCode := "session_not_found"
		if c.Terminator.DisconnectSession(command.AgentSessionID) {
			status = "succeeded"
			errorCode = ""
		}
		body, _ := json.Marshal(struct {
			Status    string `json:"status"`
			ErrorCode string `json:"errorCode,omitempty"`
		}{Status: status, ErrorCode: errorCode})
		endpoint := "/api/v1/internal/network/sessions/commands/" + url.PathEscape(command.CommandID) + "/ack"
		ack, err := c.request(ctx, http.MethodPost, endpoint, string(body))
		if err != nil {
			return err
		}
		ack.Body.Close()
		if ack.StatusCode != http.StatusOK {
			return fmt.Errorf("acknowledge disconnect: HTTP %d", ack.StatusCode)
		}
	}
	return nil
}
