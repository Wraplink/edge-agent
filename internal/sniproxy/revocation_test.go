package sniproxy

import (
	"net"
	"testing"
	"time"
)

func TestRemoveClosesActiveSessions(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	proxy := &Proxy{
		targets: map[string]string{"game.example": "203.0.113.10:443"},
		sessions: map[string]map[net.Conn]struct{}{
			"game.example": {server: {}},
		},
	}
	proxy.Remove("GAME.EXAMPLE.")
	if _, exists := proxy.targets["game.example"]; exists {
		t.Fatal("route should be removed")
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	var buf [1]byte
	if _, err := client.Read(buf[:]); err == nil {
		t.Fatal("expected active session to be closed")
	}
}

func TestRemoveDoesNotCloseOtherDomains(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	proxy := &Proxy{
		targets: map[string]string{
			"one.example": "203.0.113.10:443",
			"two.example": "203.0.113.11:443",
		},
		sessions: map[string]map[net.Conn]struct{}{
			"two.example": {b: {}},
		},
	}
	proxy.Remove("one.example")
	if _, exists := proxy.targets["two.example"]; !exists {
		t.Fatal("unrelated route should remain")
	}
	_ = a.SetWriteDeadline(time.Now().Add(time.Second))
	done := make(chan error, 1)
	go func() {
		var buf [1]byte
		_, err := b.Read(buf[:])
		done <- err
	}()
	if _, err := a.Write([]byte{1}); err != nil {
		t.Fatalf("unrelated connection closed: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
