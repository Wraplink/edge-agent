//go:build linux

package dataplane

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/client"
)

// This test must run in an isolated network namespace with CAP_NET_ADMIN.
// It never touches the host namespace when invoked through the documented runner.
func TestKernelEntitlementExpiresWithoutRefresh(t *testing.T) {
	if os.Getenv("WRAPLINK_KERNEL_TEST") != "1" {
		t.Skip("requires WRAPLINK_KERNEL_TEST=1 in an isolated network namespace")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root or appropriate netlink capabilities")
	}
	if err := exec.Command("nft", "--version").Run(); err != nil {
		t.Skip("nft binary unavailable")
	}

	n, err := NewNFT()
	if err != nil {
		t.Fatalf("initialize isolated nftables table: %v", err)
	}
	ctx := context.Background()
	clientIP := "192.0.2.51"
	expiration := time.Now().Add(3 * time.Second)
	if err := n.ReplaceAllowedSources(ctx, true, []client.AccessGrant{{
		ClientIP:  clientIP,
		ExpiresAt: expiration,
	}}); err != nil {
		t.Fatalf("add expiring entitlement: %v", err)
	}
	assertPresent := func(want bool) {
		t.Helper()
		elements, err := n.conn.GetSetElements(n.entitledSet)
		if err != nil {
			t.Fatalf("read entitlement set: %v", err)
		}
		found := false
		for _, element := range elements {
			if len(element.Key) == 4 && element.Key[0] == 192 && element.Key[1] == 0 && element.Key[2] == 2 && element.Key[3] == 51 {
				found = true
			}
		}
		if found != want {
			t.Fatalf("entitlement presence = %t, want %t", found, want)
		}
	}
	assertPresent(true)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		elements, err := n.conn.GetSetElements(n.entitledSet)
		if err != nil {
			t.Fatalf("read expiring set: %v", err)
		}
		found := false
		for _, element := range elements {
			if len(element.Key) == 4 && element.Key[0] == 192 && element.Key[1] == 0 && element.Key[2] == 2 && element.Key[3] == 51 {
				found = true
			}
		}
		if !found {
			return
		}
	}
	t.Fatal(errors.New("kernel retained expired entitlement without refresh"))
}
