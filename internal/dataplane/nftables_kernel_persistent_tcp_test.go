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

// The peer process keeps a single TCP connection open across revocation.
func TestKernelPersistentTCPPeer(t *testing.T) {
	if os.Getenv("WRAPLINK_PERSISTENT_TCP_PEER") != "1" {
		t.Skip("peer helper")
	}
	port, err := strconv.Atoi(os.Getenv("WRAPLINK_PEER_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("198.18.0.2")}, &net.TCPAddr{IP: net.ParseIP("198.18.0.1"), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Println("CONNECTED")
	input := bufio.NewScanner(os.Stdin)
	for input.Scan() {
		if _, err := conn.Write([]byte(input.Text()+"\n")); err != nil {
			fmt.Println("WRITE_FAILED")
			continue
		}
		fmt.Println("SENT")
	}
	if err := input.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestKernelPreroutingEstablishedTCPRevocation(t *testing.T) {
	if os.Getenv("WRAPLINK_KERNEL_TEST") != "1" {
		t.Skip("requires isolated network namespace")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires CAP_NET_ADMIN")
	}
	name := fmt.Sprintf("wraplink-tcp-%d", os.Getpid())
	run := func(args ...string) {
		t.Helper()
		if output, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
			t.Fatalf("ip %v: %v: %s", args, err, output)
		}
	}
	run("netns", "add", name)
	defer func() { _ = exec.Command("ip", "netns", "del", name).Run() }()
	run("link", "add", "wlt-host", "type", "veth", "peer", "name", "wlt-peer")
	defer func() { _ = exec.Command("ip", "link", "del", "wlt-host").Run() }()
	run("link", "set", "wlt-peer", "netns", name)
	run("addr", "add", "198.18.0.1/24", "dev", "wlt-host")
	run("link", "set", "wlt-host", "up")
	run("-n", name, "addr", "add", "198.18.0.2/24", "dev", "wlt-peer")
	run("-n", name, "link", "set", "wlt-peer", "up")
	run("-n", name, "link", "set", "lo", "up")
	run("addr", "add", "198.19.0.1/32", "dev", "lo")
	defer func() { _ = exec.Command("ip", "addr", "del", "198.19.0.1/32", "dev", "lo").Run() }()

	n, err := NewNFT()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := n.ReplaceAllowedSources(ctx, true, []client.AccessGrant{{ClientIP: "198.18.0.2", ExpiresAt: time.Now().Add(time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	lease := client.Lease{ID: "persistent-tcp", Address: "198.18.0.1", DestinationIP: "198.19.0.1", Protocol: "tcp"}
	if err := n.AddRoute(ctx, lease); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = n.RemoveRoute(ctx, lease) }()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("198.19.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	cmd := exec.Command("ip", "netns", "exec", name, os.Args[0], "-test.run=^TestKernelPersistentTCPPeer$", "-test.v")
	cmd.Env = append(os.Environ(), "WRAPLINK_PERSISTENT_TCP_PEER=1", "WRAPLINK_PEER_PORT="+strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
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
	for {
		line, err := ack.ReadString('\n')
		if err != nil {
			t.Fatalf("waiting for TCP connection: %v", err)
		}
		if strings.TrimSpace(line) == "CONNECTED" {
			break
		}
	}
	receiver, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	send := func(payload string, allowed bool) {
		t.Helper()
		if _, err := io.WriteString(stdin, payload+"\n"); err != nil {
			t.Fatal(err)
		}
		for {
			line, err := ack.ReadString('\n')
			if err != nil {
				t.Fatalf("peer send: %v", err)
			}
			if status := strings.TrimSpace(line); status == "SENT" || status == "WRITE_FAILED" {
				break
			}
		}
		if err := receiver.SetReadDeadline(time.Now().Add(750 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 64)
		count, err := receiver.Read(buf)
		if allowed {
			if err != nil || string(buf[:count]) != payload+"\n" {
				t.Fatalf("authorized TCP data missing: %q, %v", buf[:count], err)
			}
		} else if err == nil && count > 0 {
			t.Fatalf("revoked TCP data delivered: %q", buf[:count])
		}
	}
	send("before", true)
	if err := n.ReplaceAllowedSources(ctx, true, nil); err != nil {
		t.Fatal(err)
	}
	send("after", false)
}
