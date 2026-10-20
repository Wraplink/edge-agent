package sniproxy

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/client"
)

func TestListenerRejectsUnknownClient(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxy := &Proxy{Access: &Authorizer{}}
	done := make(chan error, 1)
	go func() {
		done <- proxy.Serve(ctx, listener)
	}()
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var buf [1]byte
	if _, err := conn.Read(buf[:]); err == nil {
		t.Fatal("unauthorized client connection should be closed")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SNI listener did not stop on context cancellation")
	}
}

func TestListenerRejectsConnectionsAboveLimit(t *testing.T) {
	auth := &Authorizer{}
	if err := auth.Replace(true, []client.AccessGrant{{
		ClientIP:  "127.0.0.1",
		ExpiresAt: time.Now().Add(time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxy := &Proxy{Access: auth, MaxConnections: 1}
	done := make(chan error, 1)
	go func() { done <- proxy.Serve(ctx, listener) }()
	first, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	time.Sleep(100 * time.Millisecond)
	second, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	_ = second.SetReadDeadline(time.Now().Add(time.Second))
	var buf [1]byte
	if _, err := second.Read(buf[:]); err == nil {
		t.Fatal("overload connection should be closed")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("overload connection was not rejected promptly")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not stop")
	}
}

func TestListenerClosesIncompleteClientHelloAfterTimeout(t *testing.T) {
	auth := &Authorizer{}
	if err := auth.Replace(true, []client.AccessGrant{{
		ClientIP:  "127.0.0.1",
		ExpiresAt: time.Now().Add(time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxy := &Proxy{Access: auth, ClientHelloTimeout: 100 * time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- proxy.Serve(ctx, listener) }()
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte{0x16, 0x03, 0x01}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var buf [1]byte
	if _, err := conn.Read(buf[:]); err == nil {
		t.Fatal("incomplete ClientHello connection should close")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("incomplete ClientHello was not closed before deadline")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not stop")
	}
}

func TestShutdownClosesPendingClientHello(t *testing.T) {
	auth := &Authorizer{}
	if err := auth.Replace(true, []client.AccessGrant{{
		ClientIP:  "127.0.0.1",
		ExpiresAt: time.Now().Add(time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	proxy := &Proxy{Access: auth, ClientHelloTimeout: time.Minute}
	done := make(chan error, 1)
	go func() { done <- proxy.Serve(ctx, listener) }()
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte{0x16, 0x03, 0x01}); err != nil {
		cancel()
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not stop")
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var buf [1]byte
	if _, err := conn.Read(buf[:]); err == nil {
		t.Fatal("pending connection should close on shutdown")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("pending connection remained open after shutdown")
	}
}

func TestListenerReleasesCapacityAfterClientHelloTimeout(t *testing.T) {
	auth := &Authorizer{}
	if err := auth.Replace(true, []client.AccessGrant{{
		ClientIP:  "127.0.0.1",
		ExpiresAt: time.Now().Add(time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxy := &Proxy{Access: auth, MaxConnections: 1, ClientHelloTimeout: 50 * time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- proxy.Serve(ctx, listener) }()
	for i := 0; i < 5; i++ {
		conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write([]byte{0x16, 0x03, 0x01}); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		var buf [1]byte
		if _, err := conn.Read(buf[:]); err == nil {
			conn.Close()
			t.Fatal("expected incomplete handshake to close")
		}
		conn.Close()
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not stop")
	}
}

func TestConcurrentShutdownClosesPendingClients(t *testing.T) {
	auth := &Authorizer{}
	if err := auth.Replace(true, []client.AccessGrant{{
		ClientIP:  "127.0.0.1",
		ExpiresAt: time.Now().Add(time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	proxy := &Proxy{Access: auth, MaxConnections: 32, ClientHelloTimeout: time.Minute}
	done := make(chan error, 1)
	go func() { done <- proxy.Serve(ctx, listener) }()
	connections := make([]net.Conn, 0, 32)
	for i := 0; i < 32; i++ {
		conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
		if err != nil {
			break
		}
		connections = append(connections, conn)
		_, _ = conn.Write([]byte{0x16, 0x03, 0x01})
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not stop")
	}
	proxy.mu.RLock()
	remaining := len(proxy.active)
	proxy.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("expected all handlers to exit, %d connections remain", remaining)
	}
	for _, conn := range connections {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		var buf [1]byte
		if _, err := conn.Read(buf[:]); err == nil {
			t.Error("connection remained open after shutdown")
		} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
			t.Error("connection timed out instead of closing")
		}
		_ = conn.Close()
	}
}
