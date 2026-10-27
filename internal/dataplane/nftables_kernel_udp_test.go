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

// Run only through scripts/test-kernel-entitlements.sh in an isolated netns.
// A connected UDP socket keeps its 5-tuple across the authorization change.
func TestKernelEstablishedUDPRevocation(t *testing.T) {
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
		ClientIP: source, ExpiresAt: time.Now().Add(time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	lease := client.Lease{
		ID: "kernel-udp-revocation", Address: "127.0.0.3",
		DestinationIP: "127.0.0.1", Protocol: "udp",
	}
	if err := n.AddRoute(ctx, lease); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = n.RemoveRoute(ctx, lease) }()

	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.LocalAddr().(*net.UDPAddr).Port
	sender, err := net.DialUDP("udp4",
		&net.UDPAddr{IP: net.ParseIP(source)},
		&net.UDPAddr{IP: net.ParseIP("127.0.0.3"), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	sendAndExpect := func(payload string, allowed bool) {
		t.Helper()
		if _, err := sender.Write([]byte(payload)); err != nil {
			t.Fatalf("send UDP packet: %v", err)
		}
		if err := listener.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 64)
		nRead, _, err := listener.ReadFromUDP(buf)
		if allowed {
			if err != nil || string(buf[:nRead]) != payload {
				t.Fatalf("authorized UDP packet not delivered: %q, %v", buf[:nRead], err)
			}
		} else if err == nil {
			t.Fatalf("revoked UDP packet delivered: %q", buf[:nRead])
		} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
			t.Fatalf("expected packet drop timeout, got: %v", err)
		}
	}
	sendAndExpect("before-revoke", true)
	if err := n.ReplaceAllowedSources(ctx, true, nil); err != nil {
		t.Fatal(err)
	}
	sendAndExpect("after-revoke", false)
}
