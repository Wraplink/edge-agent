package dataplane

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"

	"github.com/wraplink/edge-agent/internal/client"
)

const (
	tableName       = "wraplink"
	chainName       = "prerouting"
	entitledSetName = "entitled_v4"
)

type NFT struct {
	mu sync.Mutex

	conn        *nftables.Conn
	table       *nftables.Table
	chain       *nftables.Chain
	entitledSet *nftables.Set

	accessConfigured bool
	accessEnforced   bool

	routes map[string]*routeState
}

type routeState struct {
	lease client.Lease
	rules []*nftables.Rule
}

func NewNFT() (*NFT, error) {
	conn := &nftables.Conn{}

	table := conn.AddTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   tableName,
	})

	chain := conn.AddChain(&nftables.Chain{
		Table:    table,
		Name:     chainName,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityNATDest,
	})

	entitledSet := &nftables.Set{
		Table:   table,
		Name:    entitledSetName,
		KeyType: nftables.TypeIPAddr,
	}
	if err := conn.AddSet(entitledSet, nil); err != nil {
		return nil, fmt.Errorf("create entitled IPv4 set: %w", err)
	}

	if err := conn.Flush(); err != nil {
		return nil, fmt.Errorf("create nftables base: %w", err)
	}

	return &NFT{
		conn:        conn,
		table:       table,
		chain:       chain,
		entitledSet: entitledSet,
		routes:      make(map[string]*routeState),
	}, nil
}

func (n *NFT) ReplaceAllowedSources(
	ctx context.Context,
	enforced bool,
	grants []client.AccessGrant,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	elements := make([]nftables.SetElement, 0, len(grants))
	seen := make(map[string]struct{}, len(grants))
	for _, grant := range grants {
		parsed := net.ParseIP(grant.ClientIP)
		if parsed == nil {
			return fmt.Errorf("invalid entitled client IPv4 %q", grant.ClientIP)
		}
		ip := parsed.To4()
		if ip == nil {
			return fmt.Errorf("invalid entitled client IPv4 %q", grant.ClientIP)
		}
		if !grant.ExpiresAt.After(time.Now().UTC()) {
			continue
		}
		key := ip.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		elements = append(elements, nftables.SetElement{Key: []byte(ip)})
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.accessConfigured && n.accessEnforced != enforced && len(n.routes) > 0 {
		return fmt.Errorf("cannot change entitlement enforcement while route rules are active")
	}

	n.conn.FlushSet(n.entitledSet)
	if len(elements) > 0 {
		if err := n.conn.SetAddElements(n.entitledSet, elements); err != nil {
			return fmt.Errorf("buffer entitled IPv4 elements: %w", err)
		}
	}
	if err := n.conn.Flush(); err != nil {
		return fmt.Errorf("replace entitled IPv4 set: %w", err)
	}

	n.accessConfigured = true
	n.accessEnforced = enforced
	return nil
}

func (n *NFT) AddRoute(
	ctx context.Context,
	lease client.Lease,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := validateLease(lease); err != nil {
		return err
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.accessConfigured || !n.accessEnforced {
		return fmt.Errorf("route requires active source access enforcement")
	}
	if _, exists := n.routes[lease.ID]; exists {
		return nil
	}

	edgeIP := net.ParseIP(lease.Address).To4()
	destinationIP := net.ParseIP(lease.DestinationIP).To4()

	if edgeIP == nil {
		return fmt.Errorf("invalid edge IPv4 address %q", lease.Address)
	}
	if destinationIP == nil {
		return fmt.Errorf("invalid destination IPv4 address %q", lease.DestinationIP)
	}

	protocols := []string{lease.Protocol}
	if lease.Protocol == "tcp_udp" { protocols = []string{"tcp", "udp"} }

	portStart, portEnd := destinationPortRange(lease)
	var rules []*nftables.Rule
	for _, protocolName := range protocols {
	protocol, err := protocolNumber(protocolName)
	if err != nil { return err }
	exprs := make([]expr.Any, 0, 16)
	if n.accessEnforced {
		exprs = append(exprs,
			// IPv4 source address is at offset 12 in the network header.
			&expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseNetworkHeader,
				Offset:       12,
				Len:          4,
			},
			&expr.Lookup{
				SourceRegister: 1,
				SetName:        n.entitledSet.Name,
				SetID:          n.entitledSet.ID,
			},
		)
	}

	exprs = append(exprs,
		&expr.Payload{
			DestRegister: 1,
			Base:         expr.PayloadBaseNetworkHeader,
			Offset:       16,
			Len:          4,
		},
		&expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     edgeIP,
		},
		&expr.Meta{
			Key:      expr.MetaKeyL4PROTO,
			Register: 1,
		},
		&expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     []byte{protocol},
		},
	)

	if portStart != 0 || portEnd != 0 {
		exprs = append(exprs,
			&expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseTransportHeader,
				Offset:       2,
				Len:          2,
			},
			&expr.Cmp{
				Op:       expr.CmpOpGte,
				Register: 1,
				Data:     binaryutil.BigEndian.PutUint16(portStart),
			},
			&expr.Cmp{
				Op:       expr.CmpOpLte,
				Register: 1,
				Data:     binaryutil.BigEndian.PutUint16(portEnd),
			},
		)
	}

	exprs = append(exprs,
		&expr.Immediate{
			Register: 1,
			Data:     destinationIP,
		},
	)

	if portStart != 0 || portEnd != 0 {
		exprs = append(exprs,
			&expr.Immediate{
				Register: 2,
				Data:     binaryutil.BigEndian.PutUint16(portStart),
			},
		)
	}

	nat := &expr.NAT{
		Type:       expr.NATTypeDestNAT,
		Family:     uint32(nftables.TableFamilyIPv4),
		RegAddrMin: 1,
	}

	if portStart != 0 || portEnd != 0 {
		nat.RegProtoMin = 2
		if portEnd != portStart {
			exprs = append(exprs,
				&expr.Immediate{
					Register: 3,
					Data:     binaryutil.BigEndian.PutUint16(portEnd),
				},
			)
			nat.RegProtoMax = 3
		}
	}

	exprs = append(exprs, nat)

	rule := &nftables.Rule{
		Table: n.table,
		Chain: n.chain,
		Exprs: exprs,
	}

	n.conn.AddRule(rule)
	rules = append(rules, rule)
	}
	if err := n.conn.Flush(); err != nil {
		return fmt.Errorf("add route %s: %w", lease.ID, err)
	}

	n.routes[lease.ID] = &routeState{
		lease: lease,
		rules: rules,
	}
	return nil
}

func (n *NFT) RemoveRoute(
	ctx context.Context,
	lease client.Lease,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	state, exists := n.routes[lease.ID]
	if !exists {
		return nil
	}

	for _, rule := range state.rules {
		if rule == nil || rule.Handle == 0 { return fmt.Errorf("route %s has no rule handle", lease.ID) }
		if err := n.conn.DelRule(rule); err != nil { return fmt.Errorf("delete route %s: %w", lease.ID, err) }
	}

	if err := n.conn.Flush(); err != nil {
		return fmt.Errorf("flush route deletion %s: %w", lease.ID, err)
	}

	delete(n.routes, lease.ID)
	return nil
}

func validateLease(lease client.Lease) error {
	if lease.ID == "" {
		return fmt.Errorf("lease id is required")
	}
	if lease.Address == "" {
		return fmt.Errorf("lease address is required")
	}
	if lease.DestinationIP == "" {
		return fmt.Errorf("destination ip is required")
	}

	edgeIP := net.ParseIP(lease.Address).To4()
	if edgeIP == nil {
		return fmt.Errorf("invalid edge IPv4 address %q", lease.Address)
	}

	destinationIP := net.ParseIP(lease.DestinationIP).To4()
	if destinationIP == nil {
		return fmt.Errorf("invalid destination IPv4 address %q", lease.DestinationIP)
	}

	if lease.Protocol == "" {
		return fmt.Errorf("protocol is required")
	}

	switch lease.Protocol {
	case "tcp", "udp", "tcp_udp":
	default:
		return fmt.Errorf("unsupported protocol %q", lease.Protocol)
	}

	start, end := destinationPortRange(lease)
	if start == 0 && end == 0 {
		return nil
	}
	if start == 0 || end == 0 {
		return fmt.Errorf("destination port range must specify both start and end")
	}
	if start > end {
		return fmt.Errorf("destination port range is invalid: %d-%d", start, end)
	}
	return nil
}

func protocolNumber(protocol string) (byte, error) {
	switch protocol {
	case "tcp":
		return unix.IPPROTO_TCP, nil
	case "udp":
		return unix.IPPROTO_UDP, nil
	default:
		return 0, fmt.Errorf("unsupported protocol %q", protocol)
	}
}

func destinationPortRange(lease client.Lease) (uint16, uint16) {
	start := lease.DestinationPortStart
	end := lease.DestinationPortEnd
	if start == 0 && end == 0 {
		return 0, 0
	}
	return start, end
}
