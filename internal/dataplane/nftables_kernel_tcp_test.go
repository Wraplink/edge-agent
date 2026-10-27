//go:build linux

package dataplane

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/client"
)

// Run only in an isolated Linux network namespace with CAP_NET_ADMIN.
// The same established TCP socket must stop delivering bytes after revocation.
func TestKernelEstablishedTCPRevocation(t *testing.T) {
	if os.Getenv("WRAPLINK_KERNEL_TEST") != "1" {
		t.Skip("requires isolated network namespace")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root or CAP_NET_ADMIN")
	}
	ctx := context.Background()
	n, err := NewNFT(true)
	if err != nil {
		t.Fatal(err)
	}
	const source = "127.0.0.2"
	if err := n.ReplaceAllowedSources(ctx, true, []client.AccessGrant{{
		ClientIP:  source,
		ExpiresAt: time.Now().Add(time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	lease := client.Lease{
		ID: "kernel-tcp-revocation", Address: "127.0.0.3",
		DestinationIP: "127.0.0.1", Protocol: "tcp",
	}
	if err := n.AddRoute(ctx, lease); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = n.RemoveRoute(ctx, lease) }()

	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	sender, err := net.DialTCP("tcp4",
		&net.TCPAddr{IP: net.ParseIP(source)},
		&net.TCPAddr{IP: net.ParseIP("127.0.0.3"), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	receiver, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()

	if _, err := sender.Write([]byte("before")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	count, err := receiver.Read(buf)
	if err != nil || string(buf[:count]) != "before" {
		t.Fatalf("authorized TCP data not received: %q, %v", buf[:count], err)
	}

	if err := n.ReplaceAllowedSources(ctx, true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := sender.Write([]byte("after")); err != nil {
		// A revoked connection may fail immediately; this is acceptable.
		return
	}
	if err := receiver.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	count, err = receiver.Read(buf)
	if err == nil && count > 0 {
		t.Fatalf("revoked TCP data delivered: %q", buf[:count])
	}
	if err == nil {
		t.Fatal("unexpected successful read after revocation")
	}
}
