package sniproxy

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/wraplink/edge-agent/internal/client"
)

// Authorizer maintains the currently valid source IP entitlements.
// A zero-value Authorizer denies all clients.
type Authorizer struct {
	mu sync.RWMutex
	enforced bool
	grants map[string]time.Time
}

func (a *Authorizer) Replace(enforced bool, grants []client.AccessGrant) error {
	next := make(map[string]time.Time, len(grants))
	for _, grant := range grants {
		ip := net.ParseIP(grant.ClientIP)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("invalid client IPv4 %q", grant.ClientIP)
		}
		key := ip.To4().String()
		if expiry, ok := next[key]; !ok || grant.ExpiresAt.After(expiry) {
			next[key] = grant.ExpiresAt
		}
	}
	a.mu.Lock()
	a.enforced = enforced
	a.grants = next
	a.mu.Unlock()
	return nil
}

func (a *Authorizer) Allowed(ip string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil || parsed.To4() == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.enforced {
		return false
	}
	expiry, ok := a.grants[parsed.To4().String()]
	return ok && time.Now().Before(expiry)
}
