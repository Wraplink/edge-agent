package sniproxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// Proxy forwards TLS streams based on the client's SNI without decrypting TLS.
// Only explicitly registered destinations are routable.
type Proxy struct {
	Access            *Authorizer
	DialTimeout       time.Duration
	MaxConnections    int
	ClientHelloTimeout time.Duration
	mu                sync.RWMutex
	targets           map[string]string
	sessions          map[string]map[net.Conn]struct{}
}

func (p *Proxy) Register(domain, destination string) error {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" || destination == "" {
		return fmt.Errorf("SNI domain and destination are required")
	}
	host, port, err := net.SplitHostPort(destination)
	if err != nil {
		return fmt.Errorf("invalid SNI destination: %w", err)
	}
	parsedIP := net.ParseIP(host)
	if parsedIP == nil {
		return fmt.Errorf("SNI destination must be an IP literal")
	}
	if parsedIP.IsUnspecified() || parsedIP.IsMulticast() || parsedIP.IsLoopback() || parsedIP.IsLinkLocalUnicast() || parsedIP.IsLinkLocalMulticast() {
		return fmt.Errorf("SNI destination cannot be a loopback, link-local, multicast or unspecified IP")
	}
	if port != "443" {
		return fmt.Errorf("SNI destination must use TLS port 443")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.targets == nil {
		p.targets = make(map[string]string)
	}
	previous := p.targets[domain]
	p.targets[domain] = destination
	if previous != "" && previous != destination {
		for conn := range p.sessions[domain] {
			_ = conn.Close()
		}
	}
	return nil
}

func (p *Proxy) Remove(domain string) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	p.mu.Lock()
	delete(p.targets, domain)
	var connections []net.Conn
	for conn := range p.sessions[domain] {
		connections = append(connections, conn)
	}
	p.mu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
}

func (p *Proxy) Serve(ctx context.Context, listener net.Listener) error {
	if p.Access == nil {
		return fmt.Errorf("SNI authorization is required")
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	limit := p.MaxConnections
	if limit <= 0 {
		limit = 1024
	}
	slots := make(chan struct{}, limit)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
			go func() {
				defer func() { <-slots }()
				p.handle(ctx, conn)
			}()
		default:
			_ = conn.Close()
		}
	}
}

func (p *Proxy) handle(ctx context.Context, raw net.Conn) {
	defer raw.Close()
	host, _, err := net.SplitHostPort(raw.RemoteAddr().String())
	if err != nil || !p.Access.Allowed(host) {
		return
	}
	helloTimeout := p.ClientHelloTimeout
	if helloTimeout <= 0 {
		helloTimeout = 5 * time.Second
	}
	_ = raw.SetReadDeadline(time.Now().Add(helloTimeout))
	var hello []byte
	const maxHello = 65536
	var serverName string
	for len(hello) < maxHello {
		chunk := make([]byte, 4096)
		n, err := raw.Read(chunk)
		if err != nil {
			return
		}
		hello = append(hello, chunk[:n]...)
		name, complete := clientHelloName(hello)
		if complete {
			serverName = name
			break
		}
	}
	if serverName == "" {
		return
	}
	p.mu.RLock()
	target := p.targets[serverName]
	p.mu.RUnlock()
	if target == "" || !p.Access.Allowed(host) {
		return
	}
	p.mu.Lock()
	if p.targets[serverName] != target || !p.Access.Allowed(host) {
		p.mu.Unlock()
		return
	}
	if p.sessions == nil {
		p.sessions = make(map[string]map[net.Conn]struct{})
	}
	if p.sessions[serverName] == nil {
		p.sessions[serverName] = make(map[net.Conn]struct{})
	}
	p.sessions[serverName][raw] = struct{}{}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.sessions[serverName], raw)
		if len(p.sessions[serverName]) == 0 {
			delete(p.sessions, serverName)
		}
		p.mu.Unlock()
	}()
	timeout := p.DialTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	upstream, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", target)
	if err != nil {
		return
	}
	defer upstream.Close()
	_ = raw.SetReadDeadline(time.Time{})
	for len(hello) > 0 {
		n, err := upstream.Write(hello)
		if err != nil || n == 0 {
			return
		}
		hello = hello[n:]
	}
	// Recheck authorization during an established session so expiry and
	// disconnect snapshots terminate traffic without waiting for TCP close.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				_ = raw.Close()
				_ = upstream.Close()
				return
			case <-ticker.C:
				p.mu.RLock()
				currentTarget := p.targets[serverName]
				p.mu.RUnlock()
				if !p.Access.Allowed(host) || currentTarget != target {
					_ = raw.Close()
					_ = upstream.Close()
					return
				}
			}
		}
	}()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(upstream, raw)
		_ = upstream.Close()
		close(done)
	}()
	_, _ = io.Copy(raw, upstream)
	_ = raw.Close()
	<-done
}

// clientHelloName parses SNI using Go's TLS ClientHello parser without
// performing a TLS handshake or terminating the encrypted connection.
func clientHelloName(data []byte) (string, bool) {
	var name string
	conn := &helloReader{data: data}
	server := tls.Server(conn, &tls.Config{
		GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
			name = strings.ToLower(strings.TrimSuffix(info.ServerName, "."))
			return nil, fmt.Errorf("SNI inspection complete")
		},
	})
	_ = server.Handshake()
	return name, name != ""
}

type helloReader struct {
	data []byte
}

func (r *helloReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func (r *helloReader) Write(p []byte) (int, error) {
	return len(p), nil
}

func (r *helloReader) Close() error {
	return nil
}

func (r *helloReader) LocalAddr() net.Addr {
	return &net.TCPAddr{}
}

func (r *helloReader) RemoteAddr() net.Addr {
	return &net.TCPAddr{}
}

func (r *helloReader) SetDeadline(time.Time) error {
	return nil
}

func (r *helloReader) SetReadDeadline(time.Time) error {
	return nil
}

func (r *helloReader) SetWriteDeadline(time.Time) error {
	return nil
}
