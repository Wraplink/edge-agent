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
	Access *Authorizer
	DialTimeout time.Duration
	mu sync.RWMutex
	targets map[string]string
}

func (p *Proxy) Register(domain, destination string) error {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if domain == "" || destination == "" {
		return fmt.Errorf("SNI domain and destination are required")
	}
	if _, _, err := net.SplitHostPort(destination); err != nil {
		return fmt.Errorf("invalid SNI destination: %w", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.targets == nil {
		p.targets = make(map[string]string)
	}
	p.targets[domain] = destination
	return nil
}

func (p *Proxy) Remove(domain string) {
	p.mu.Lock()
	delete(p.targets, strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), ".")))
	p.mu.Unlock()
}

func (p *Proxy) Serve(ctx context.Context, listener net.Listener) error {
	if p.Access == nil {
		return fmt.Errorf("SNI authorization is required")
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go p.handle(ctx, conn)
	}
}

func (p *Proxy) handle(ctx context.Context, raw net.Conn) {
	defer raw.Close()
	host, _, err := net.SplitHostPort(raw.RemoteAddr().String())
	if err != nil || !p.Access.Allowed(host) {
		return
	}
	_ = raw.SetReadDeadline(time.Now().Add(5 * time.Second))
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
	if _, err := upstream.Write(hello); err != nil {
		return
	}
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
func (r *helloReader) Write(p []byte) (int, error) { return len(p), nil }
func (r *helloReader) Close() error { return nil }
func (r *helloReader) LocalAddr() net.Addr { return &net.TCPAddr{} }
func (r *helloReader) RemoteAddr() net.Addr { return &net.TCPAddr{} }
func (r *helloReader) SetDeadline(time.Time) error { return nil }
func (r *helloReader) SetReadDeadline(time.Time) error { return nil }
func (r *helloReader) SetWriteDeadline(time.Time) error { return nil }
