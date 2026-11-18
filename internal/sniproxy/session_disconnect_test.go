package sniproxy

import (
	"net"
	"testing"
	"time"
)

func TestDisconnectSessionIsTargeted(t *testing.T) {
	first, firstPeer := net.Pipe()
	defer first.Close()
	defer firstPeer.Close()
	second, secondPeer := net.Pipe()
	defer second.Close()
	defer secondPeer.Close()

	p := &Proxy{sessionIDs: map[string]net.Conn{"first": first, "second": second}}
	if p.DisconnectSession("unknown") {
		t.Fatal("unknown session was reported disconnected")
	}
	if !p.DisconnectSession("first") {
		t.Fatal("known session not found")
	}
	if _, err := firstPeer.Write([]byte("x")); err == nil {
		t.Fatal("disconnected peer still writable")
	}
	// A targeted disconnect must not terminate unrelated sessions.
	if err := second.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	received := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, err := secondPeer.Read(buf)
		received <- err
	}()
	if _, err := second.Write([]byte("x")); err != nil {
		t.Fatalf("unrelated session closed: %v", err)
	}
	if err := <-received; err != nil {
		t.Fatalf("unrelated session read failed: %v", err)
	}
}

func TestActiveSessionsSnapshotDoesNotExposeMutableState(t *testing.T) {
	now := time.Now().UTC()
	p := &Proxy{sessionDetails: map[string]ActiveSession{
		"one": {ID: "one", ClientIP: "192.0.2.10", Domain: "example.com", ConnectedAt: now},
	}}
	sessions := p.ActiveSessions()
	if len(sessions) != 1 || sessions[0].ID != "one" {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}
	sessions[0].ClientIP = "203.0.113.1"
	fresh := p.ActiveSessions()
	if fresh[0].ClientIP != "192.0.2.10" {
		t.Fatal("snapshot mutated internal state")
	}
}


func TestDisconnectSessionClosesClientAndUpstream(t *testing.T) {
	client, clientPeer := net.Pipe()
	defer client.Close()
	defer clientPeer.Close()
	upstream, upstreamPeer := net.Pipe()
	defer upstream.Close()
	defer upstreamPeer.Close()
	other, otherPeer := net.Pipe()
	defer other.Close()
	defer otherPeer.Close()

	p := &Proxy{
		sessionIDs: map[string]net.Conn{"target": client, "other": other},
		sessionUpstreams: map[string]net.Conn{"target": upstream},
	}
	if !p.DisconnectSession("target") {
		t.Fatal("target session not found")
	}
	if _, err := clientPeer.Write([]byte("x")); err == nil {
		t.Fatal("client peer remained writable")
	}
	if _, err := upstreamPeer.Write([]byte("x")); err == nil {
		t.Fatal("upstream peer remained writable")
	}
	if err := other.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		var buf [1]byte
		_, err := otherPeer.Read(buf[:])
		done <- err
	}()
	if _, err := other.Write([]byte("x")); err != nil {
		t.Fatalf("unrelated connection closed: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("unrelated connection failed: %v", err)
	}
}
