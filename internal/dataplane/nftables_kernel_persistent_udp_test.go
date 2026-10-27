//go:build linux

package dataplane

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/client"
)

// The peer helper keeps one connected UDP socket alive while the parent
// changes authorization in the agent namespace.
func TestKernelPersistentUDPPeer(t *testing.T) {
	if os.Getenv("WRAPLINK_PERSISTENT_UDP_PEER") != "1" {
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
	input := bufio.NewScanner(os.Stdin)
	for input.Scan() {
		if _, err := conn.Write([]byte(input.Text())); err != nil {
			t.Fatal(err)
		}
		fmt.Println("SENT")
	}
	if err := input.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestKernelPreroutingEstablishedUDPRevocation(t *testing.T) {
	if os.Getenv("WRAPLINK_KERNEL_TEST") != "1" {
		t.Skip("requires isolated network namespace")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires CAP_NET_ADMIN")
	}
	name := fmt.Sprintf("wraplink-persistent-%d", os.Getpid())
	run := func(args ...string) {
		t.Helper()
		if output, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
			t.Fatalf("ip %v: %v: %s", args, err, output)
		}
	}
	run("netns", "add", name)
	defer func() { _ = exec.Command("ip", "netns", "del", name).Run() }()
	run("link", "add", "wlp-host", "type", "veth", "peer", "name", "wlp-peer")
	defer func() { _ = exec.Command("ip", "link", "del", "wlp-host").Run() }()
	run("link", "set", "wlp-peer", "netns", name)
	run("addr", "add", "198.18.0.1/24", "dev", "wlp-host")
	run("link", "set", "wlp-host", "up")
	run("-n", name, "addr", "add", "198.18.0.2/24", "dev", "wlp-peer")
	run("-n", name, "link", "set", "wlp-peer", "up")
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
	lease := client.Lease{ID: "persistent-udp", Address: "198.18.0.1", DestinationIP: "198.19.0.1", Protocol: "udp"}
	if err := n.AddRoute(ctx, lease); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = n.RemoveRoute(ctx, lease) }()
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("198.19.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	cmd := exec.Command("ip", "netns", "exec", name, os.Args[0], "-test.run=^TestKernelPersistentUDPPeer$", "-test.v")
	cmd.Env = append(os.Environ(), "WRAPLINK_PERSISTENT_UDP_PEER=1", "WRAPLINK_PEER_PORT="+strconv.Itoa(listener.LocalAddr().(*net.UDPAddr).Port))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	ack := bufio.NewReader(stdout)
	send := func(payload string, allowed bool) {
		t.Helper()
		if _, err := io.WriteString(stdin, payload+"\n"); err != nil {
			t.Fatal(err)
		}
		// go test -v emits RUN/PAUSE/CONT output before the helper acknowledgment.
		for {
			line, err := ack.ReadString('\n')
			if err != nil {
				t.Fatalf("peer acknowledgment: %v", err)
			}
			if strings.TrimSpace(line) == "SENT" {
				break
			}
		}
		if err := listener.SetReadDeadline(time.Now().Add(750 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 64)
		count, _, err := listener.ReadFromUDP(buf)
		if allowed {
			if err != nil || string(buf[:count]) != payload {
				t.Fatalf("authorized packet missing: %q, %v", buf[:count], err)
			}
		} else if err == nil {
			t.Fatalf("revoked packet delivered: %q", buf[:count])
		} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
			t.Fatalf("expected drop timeout: %v", err)
		}
	}
	send("before", true)
	if err := n.ReplaceAllowedSources(ctx, true, nil); err != nil {
		t.Fatal(err)
	}
	send("after", false)
}
