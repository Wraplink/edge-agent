// Package origindns provides an opt-in authenticated DNS-wire endpoint for
// DNS Control origin lookups. It never uses WrapLink managed DNS policies.
package origindns

import (
	"context"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

type Handler struct {
	Token     string
	Upstreams []string
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/dns/resolve" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.Token)) != 1 || len(h.Token) < 32 {
		http.Error(w, "unauthorized", 401)
		return
	}
	if r.Header.Get("Content-Type") != "application/dns-message" {
		http.Error(w, "unsupported content type", 415)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	if err != nil || len(payload) > 4096 {
		http.Error(w, "invalid DNS request size", 400)
		return
	}
	question, id, err := parseQuestion(payload, false)
	if err != nil {
		http.Error(w, "invalid DNS question", 400)
		return
	}
	if question.Type != dnsmessage.TypeA && question.Type != dnsmessage.TypeAAAA && question.Type != dnsmessage.TypeCNAME {
		http.Error(w, "unsupported DNS question type", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var response []byte
	for _, server := range h.Upstreams {
		response, err = exchange(ctx, server, payload)
		if err != nil {
			continue
		}
		returned, replyID, parseErr := parseQuestion(response, true)
		if parseErr != nil || replyID != id || returned != question {
			err = fmt.Errorf("upstream DNS response mismatch")
			continue
		}
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(response)
		return
	}
	http.Error(w, "upstream DNS unavailable", http.StatusBadGateway)
}

func parseQuestion(wire []byte, expectResponse bool) (dnsmessage.Question, uint16, error) {
	var parser dnsmessage.Parser
	header, err := parser.Start(wire)
	if err != nil {
		return dnsmessage.Question{}, 0, err
	}
	if header.Response != expectResponse || header.OpCode != 0 {
		return dnsmessage.Question{}, 0, fmt.Errorf("unexpected DNS message type")
	}
	question, err := parser.Question()
	if err != nil {
		return dnsmessage.Question{}, 0, err
	}
	if _, err = parser.Question(); err != dnsmessage.ErrSectionDone {
		return dnsmessage.Question{}, 0, fmt.Errorf("multiple DNS questions")
	}
	if question.Class != dnsmessage.ClassINET {
		return dnsmessage.Question{}, 0, fmt.Errorf("unsupported DNS class")
	}
	return question, header.ID, nil
}

func exchange(ctx context.Context, server string, query []byte) ([]byte, error) {
	host, port, err := net.SplitHostPort(server)
	if err != nil || net.ParseIP(host) == nil || port != "53" {
		return nil, fmt.Errorf("invalid upstream address")
	}
	// DNS over TCP avoids UDP truncation and requires no third-party DNS library.
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline, ok := ctx.Deadline()
	if ok {
		_ = conn.SetDeadline(deadline)
	}
	frame := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(query)))
	copy(frame[2:], query)
	if _, err = conn.Write(frame); err != nil {
		return nil, err
	}
	var length [2]byte
	if _, err = io.ReadFull(conn, length[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(length[:]))
	if n < 12 || n > 65535 {
		return nil, fmt.Errorf("invalid upstream response length")
	}
	answer := make([]byte, n)
	if _, err = io.ReadFull(conn, answer); err != nil {
		return nil, err
	}
	return answer, nil
}

// ValidateUpstreams prevents accidental forwarding to local/hijacked DNS.
func ValidateUpstreams(upstreams []string) error {
	if len(upstreams) == 0 {
		return fmt.Errorf("origin DNS requires upstream resolvers")
	}
	for _, server := range upstreams {
		host, port, err := net.SplitHostPort(strings.TrimSpace(server))
		ip := net.ParseIP(host)
		if err != nil || ip == nil || port != "53" || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			return fmt.Errorf("origin DNS upstream must be an independent IP:53 resolver")
		}
	}
	return nil
}
