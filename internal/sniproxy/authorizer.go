package sniproxy

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wraplink/edge-agent/internal/client"
)

// Authorizer maintains the currently valid source IP entitlements.
// A zero-value Authorizer denies all clients.
type Authorizer struct {
	mu       sync.RWMutex
	enforced bool
	grants   map[string]time.Time
	version  uint64
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
	a.version++
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

func (a *Authorizer) Version() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.version
}

// AccessEntry describes a currently effective source-IP grant.
type AccessEntry struct {
	ClientIP  string    `json:"client_ip"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Snapshot returns a copy without exposing internal mutable state.
func (a *Authorizer) Snapshot() (bool, uint64, []AccessEntry) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	entries := make([]AccessEntry, 0, len(a.grants))
	now := time.Now()
	if a.enforced {
		for ip, expiry := range a.grants {
			if now.Before(expiry) {
				entries = append(entries, AccessEntry{ClientIP: ip, ExpiresAt: expiry})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ClientIP < entries[j].ClientIP })
	return a.enforced, a.version, entries
}
