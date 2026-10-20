package sniproxy

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/client"
)

func TestProxyForwardsTLSClientHelloToUpstream(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	received := make(chan []byte, 1)
	go func() {
		conn, err := upstream.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var buf [4096]byte
		n, err := conn.Read(buf[:])
		if err == nil {
			received <- append([]byte(nil), buf[:n]...)
		}
	}()

	// The public Register API intentionally limits destinations to port 443.
	// Inject a local test destination directly to avoid binding privileged ports.
	auth := &Authorizer{}
	if err := auth.Replace(true, []client.AccessGrant{{
		ClientIP:  "127.0.0.1",
		ExpiresAt: time.Now().Add(time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	proxy := &Proxy{
		Access: auth,
		targets: map[string]string{"game.example": upstream.Addr().String()},
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- proxy.Serve(ctx, listener) }()
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: "game.example",
		InsecureSkipVerify: true, // No TLS server in this forwarding-only test.
	})
	_ = tlsConn.SetDeadline(time.Now().Add(3 * time.Second))
	_ = tlsConn.Handshake()
	select {
	case data := <-received:
		name, complete := clientHelloName(data)
		if !complete || name != "game.example" {
			t.Fatalf("upstream received unexpected TLS hello: %q", name)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("upstream did not receive TLS ClientHello")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not stop")
	}
}
