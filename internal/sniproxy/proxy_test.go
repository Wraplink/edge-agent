package sniproxy

import (
	"net"
	"sync"
	"testing"
)

func TestProxyRegisterRequiresExplicitTLSDestination(t *testing.T) {
	var p Proxy
	for _, destination := range []string{"", "example.com:443", "192.0.2.5:80", "bad-address", "127.0.0.1:443", "0.0.0.0:443", "169.254.1.1:443", "224.0.0.1:443", "[::1]:443"} {
		if err := p.Register("example.org", destination); err == nil {
			t.Fatalf("expected destination %q to be rejected", destination)
		}
	}
	if err := p.Register("example.org", "192.0.2.5:443"); err != nil {
		t.Fatalf("valid TLS destination rejected: %v", err)
	}
	p.Remove("example.org")
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.targets) != 0 {
		t.Fatal("removing SNI domain must remove forwarding destination")
	}
}

func TestConcurrentRouteUpdatesPreserveLatestDestination(t *testing.T) {
	var p Proxy
	const domain = "example.org"
	const oldTarget = "192.0.2.5:443"
	const newTarget = "192.0.2.6:443"
	if err := p.Register(domain, oldTarget); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				p.Remove(domain)
				if err := p.Register(domain, oldTarget); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	workers.Wait()
	if err := p.Register(domain, newTarget); err != nil {
		t.Fatal(err)
	}
	p.mu.RLock()
	got := p.targets[domain]
	p.mu.RUnlock()
	if got != newTarget {
		t.Fatalf("final route = %q, want %q", got, newTarget)
	}
}

func TestRemoveClosesTrackedSession(t *testing.T) {
	var p Proxy
	if err := p.Register("example.org", "192.0.2.5:443"); err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	defer server.Close()
	defer client.Close()
	p.mu.Lock()
	p.sessions = map[string]map[net.Conn]struct{}{
		"example.org": {client: {}},
	}
	p.mu.Unlock()
	p.Remove("example.org")
	if _, err := client.Write([]byte("test")); err == nil {
		t.Fatal("removed route left its session open")
	}
	p.mu.RLock()
	_, exists := p.targets["example.org"]
	p.mu.RUnlock()
	if exists {
		t.Fatal("removed route still registered")
	}
}
