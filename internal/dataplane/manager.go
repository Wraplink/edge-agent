package dataplane

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/wraplink/edge-agent/internal/client"
)

type Manager struct {
	mu sync.Mutex

	nft *NFT

	active map[string]client.Lease
}

func NewManager(localOutput ...bool) (*Manager, error) {
	nft, err := NewNFT(localOutput...)
	if err != nil {
		return nil, err
	}

	return &Manager{
		nft:    nft,
		active: make(map[string]client.Lease),
	}, nil
}

func (m *Manager) ReplaceAccessGrants(
	ctx context.Context,
	enforced bool,
	grants []client.AccessGrant,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.nft == nil {
		return fmt.Errorf("nft dataplane is not configured")
	}

	if err := m.nft.ReplaceAllowedSources(ctx, enforced, grants); err != nil {
		return fmt.Errorf("replace nft access grants: %w", err)
	}
	return nil
}

func (m *Manager) Apply(
	ctx context.Context,
	lease client.Lease,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Only route leases are handled by nftables.
	if lease.Type != "route" {
		return nil
	}

	if lease.ID == "" {
		return fmt.Errorf("lease id is required")
	}

	if lease.Address == "" {
		return fmt.Errorf("lease %s has no edge address", lease.ID)
	}
	if lease.DestinationIP == "" {
		return fmt.Errorf("lease %s has no destination IP", lease.ID)
	}
	if lease.Protocol != "tcp" && lease.Protocol != "udp" && lease.Protocol != "tcp_udp" {
		return fmt.Errorf("lease %s has invalid protocol %q", lease.ID, lease.Protocol)
	}

	if lease.DestinationPortStart != 0 || lease.DestinationPortEnd != 0 {
		if lease.DestinationPortStart == 0 {
			return fmt.Errorf("lease %s has invalid destination port range: start is 0", lease.ID)
		}
		if lease.DestinationPortEnd == 0 {
			return fmt.Errorf("lease %s has invalid destination port range: end is 0", lease.ID)
		}
		if lease.DestinationPortStart > lease.DestinationPortEnd {
			return fmt.Errorf(
				"lease %s has invalid destination port range: %d-%d",
				lease.ID,
				lease.DestinationPortStart,
				lease.DestinationPortEnd,
			)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if previous, exists := m.active[lease.ID]; exists && reflect.DeepEqual(previous, lease) {
		return nil
	}

	if err := m.nft.AddRoute(ctx, lease); err != nil {
		return fmt.Errorf("add nft route %s: %w", lease.ID, err)
	}

	m.active[lease.ID] = lease
	return nil
}

func (m *Manager) Remove(
	ctx context.Context,
	leaseID string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if leaseID == "" {
		return fmt.Errorf("lease id is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	lease, ok := m.active[leaseID]
	if !ok {
		return nil
	}

	if err := m.nft.RemoveRoute(ctx, lease); err != nil {
		return fmt.Errorf("remove nft route %s: %w", leaseID, err)
	}

	delete(m.active, leaseID)
	return nil
}
