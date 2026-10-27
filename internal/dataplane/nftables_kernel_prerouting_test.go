//go:build linux

package dataplane

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/client"
)

// TestKernelPeerUDPSend runs in a separate namespace as a subprocess of the test binary.
func TestKernelPeerUDPSend(t *testing.T) {
	if os.Getenv("WRAPLINK_PEER_SEND") != "1" {
		t.Skip("peer helper")
	}
	port, err := strconv.Atoi(os.Getenv("WRAPLINK_PEER_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialUDP("udp4", &net.UDPAddr{IP: net.ParseIP("198.18.0.2")}, &net.UDPAddr{IP: net.ParseIP("198.18.0.1"), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(os.Getenv("WRAPLINK_PEER_PAYLOAD"))); err != nil {
		t.Fatal(err)
	}
}

// The client is in a separate namespace: packets traverse PREROUTING rather than OUTPUT.
func TestKernelPreroutingUDPRevocation(t *testing.T) {
	if os.Getenv("WRAPLINK_KERNEL_TEST") != "1" {
		t.Skip("requires isolated network namespace")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires CAP_NET_ADMIN")
	}
	name := fmt.Sprintf("wraplink-peer-%d", os.Getpid())
	run := func(args ...string) {
		t.Helper()
		if output, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
			t.Fatalf("ip %v: %v: %s", args, err, output)
		}
	}
	run("netns", "add", name)
	defer func() { _ = exec.Command("ip", "netns", "del", name).Run() }()
	run("link", "add", "wl-host", "type", "veth", "peer", "name", "wl-peer")
	defer func() { _ = exec.Command("ip", "link", "del", "wl-host").Run() }()
	run("link", "set", "wl-peer", "netns", name)
	run("addr", "add", "198.18.0.1/24", "dev", "wl-host")
	run("link", "set", "wl-host", "up")
	run("-n", name, "addr", "add", "198.18.0.2/24", "dev", "wl-peer")
	run("-n", name, "link", "set", "wl-peer", "up")
	run("-n", name, "link", "set", "lo", "up")
	run("addr", "add", "198.19.0.1/32", "dev", "lo")
	defer func() { _ = exec.Command("ip", "addr", "del", "198.19.0.1/32", "dev", "lo").Run() }()

	n, err := NewNFT()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := n.ReplaceAllowedSources(ctx, true, []client.AccessGrant{{
		ClientIP: "198.18.0.2", ExpiresAt: time.Now().Add(time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	lease := client.Lease{ID: "kernel-prerouting-udp", Address: "198.18.0.1", DestinationIP: "198.19.0.1", Protocol: "udp"}
	if err := n.AddRoute(ctx, lease); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = n.RemoveRoute(ctx, lease) }()
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("198.19.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.LocalAddr().(*net.UDPAddr).Port
	send := func(payload string, allowed bool) {
		t.Helper()
		cmd := exec.Command("ip", "netns", "exec", name, os.Args[0], "-test.run=^TestKernelPeerUDPSend$", "-test.v")
		cmd.Env = append(os.Environ(), "WRAPLINK_PEER_SEND=1", "WRAPLINK_PEER_PORT="+strconv.Itoa(port), "WRAPLINK_PEER_PAYLOAD="+payload)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("peer send failed: %v: %s", err, output)
		}
		if err := listener.SetReadDeadline(time.Now().Add(750 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 128)
		count, _, err := listener.ReadFromUDP(buf)
		if allowed {
			if err != nil || string(buf[:count]) != payload {
				t.Fatalf("authorized packet missing: %q: %v", buf[:count], err)
			}
		} else if err == nil {
			t.Fatalf("revoked packet delivered: %q", buf[:count])
		} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
			t.Fatalf("expected drop timeout: %v", err)
		}
	}
	send("authorized", true)
	if err := n.ReplaceAllowedSources(ctx, true, nil); err != nil {
		t.Fatal(err)
	}
	send("revoked", false)
}
