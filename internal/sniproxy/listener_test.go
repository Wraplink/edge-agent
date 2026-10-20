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
