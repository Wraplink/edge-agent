package sniproxy

import "testing"

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
