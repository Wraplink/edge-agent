package dataplane

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"reflect"
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
	outputChainName = "output"
	entitledSetName = "entitled_v4"
)

type NFT struct {
	mu sync.Mutex

	conn        *nftables.Conn
	table       *nftables.Table
	chain       *nftables.Chain
	outputChain *nftables.Chain
	localOutputEnabled bool
	entitledSet *nftables.Set

	accessConfigured bool
	accessEnforced   bool

	routes map[string]*routeState
}

type routeState struct {
	lease       client.Lease
	rules       []*nftables.Rule
	outputRules []*nftables.Rule
}

func NewNFT(localOutput ...bool) (*NFT, error) {
	enabled := len(localOutput) > 0 && localOutput[0]
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

	outputChain := conn.AddChain(&nftables.Chain{
		Table:    table,
		Name:     outputChainName,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookOutput,
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
		outputChain: outputChain,
		localOutputEnabled: enabled,
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
	if state, exists := n.routes[lease.ID]; exists {
		if reflect.DeepEqual(state.lease, lease) {
			return nil
		}
		if err := n.removeLeaseRules(lease.ID, state.rules); err != nil {
			return fmt.Errorf("replace changed route %s: %w", lease.ID, err)
		}
		if n.localOutputEnabled {
	if err := n.removeOutputLeaseRules(lease.ID, state.outputRules); err != nil {
			return fmt.Errorf("replace changed output route %s: %w", lease.ID, err)
		}
		}
	delete(n.routes, lease.ID)
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
	if lease.Protocol == "tcp_udp" {
		protocols = []string{"tcp", "udp"}
	}

	portStart, portEnd := destinationPortRange(lease)
	var rules []*nftables.Rule
	for _, protocolName := range protocols {
		protocol, err := protocolNumber(protocolName)
		if err != nil {
			return err
		}
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
			Table:    n.table,
			Chain:    n.chain,
			Exprs:    exprs,
			UserData: []byte("wraplink:lease:" + lease.ID + ":" + protocolName),
		}

		rules = append(rules, rule)
	}
	// Reconcile stale rules before installing replacements. Only remove rules
	// carrying this lease's tag or exact legacy expression matches.
	if err := n.removeLeaseRules(lease.ID, rules); err != nil {
		return fmt.Errorf("reconcile route %s: %w", lease.ID, err)
	}
	// Full-address routes exclusively own their allocated destination IP.
	// A prior process may have installed a different lease ID or origin IP.
	// Remove its DNAT rules before adding the current desired rules.
	if portStart == 0 && portEnd == 0 {
		if err := n.removeStaleAddressRules(edgeIP); err != nil {
			return fmt.Errorf("reconcile route address %s: %w", edgeIP, err)
		}
	}
	// OUTPUT handles connections originating on the edge host itself.
	// Source entitlement lookup is preserved in the copied expressions.
	outputRules := make([]*nftables.Rule, 0, len(rules))
	for _, rule := range rules {
		outputRules = append(outputRules, &nftables.Rule{
			Table:    n.table,
			Chain:    n.outputChain,
			Exprs:    rule.Exprs,
			UserData: rule.UserData,
		})
	}
	if n.localOutputEnabled {
	if err := n.removeOutputLeaseRules(lease.ID, outputRules); err != nil {
		return fmt.Errorf("reconcile output route %s: %w", lease.ID, err)
	}
	if portStart == 0 && portEnd == 0 {
		if err := n.removeStaleOutputAddressRules(edgeIP); err != nil {
			return fmt.Errorf("reconcile output address %s: %w", edgeIP, err)
		}
	}
	}
	for _, rule := range rules {
		n.conn.AddRule(rule)
	}
	if n.localOutputEnabled {
		for _, rule := range outputRules {
			n.conn.AddRule(rule)
		}
	}
	if err := n.conn.Flush(); err != nil {
		return fmt.Errorf("add route %s: %w", lease.ID, err)
	}

	if err := n.recoverRouteHandles(lease.ID, rules); err != nil {
		return fmt.Errorf("recover added route handles %s: %w", lease.ID, err)
	}

	if n.localOutputEnabled {
	if err := n.recoverOutputRouteHandles(lease.ID, outputRules); err != nil {
		return fmt.Errorf("recover output route handles %s: %w", lease.ID, err)
	}
	}
	n.routes[lease.ID] = &routeState{
		lease:       lease,
		rules:       rules,
		outputRules: outputRules,
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
		// Remove orphaned tagged rules left by a previous process.
		if err := n.removeLeaseRules(lease.ID, nil); err != nil {
			return err
		}
		if n.localOutputEnabled {
			return n.removeOutputLeaseRules(lease.ID, nil)
		}
		return nil
	}
	if err := n.removeLeaseRules(lease.ID, state.rules); err != nil {
		return fmt.Errorf("delete route %s: %w", lease.ID, err)
	}

	if err := n.removeOutputLeaseRules(lease.ID, state.outputRules); err != nil {
		return err
	}
	delete(n.routes, lease.ID)
	return nil
}

// removeLeaseRules removes all rules owned by a lease, including duplicates
// left by failed installations or previous agent processes. Legacy untagged
// rules are removed only when their full expression sequence matches.
func (n *NFT) removeLeaseRules(leaseID string, wanted []*nftables.Rule) error {
	installed, err := n.conn.GetRules(n.table, n.chain)
	if err != nil {
		return fmt.Errorf("list nftables rules: %w", err)
	}
	prefix := []byte("wraplink:lease:" + leaseID + ":")
	removed := 0
	for _, candidate := range installed {
		if candidate.Handle == 0 {
			continue
		}
		owned := bytes.HasPrefix(candidate.UserData, prefix)
		if !owned && len(candidate.UserData) == 0 {
			for _, rule := range wanted {
				if rule != nil && reflect.DeepEqual(rule.Exprs, candidate.Exprs) {
					owned = true
					break
				}
			}
		}
		if !owned {
			continue
		}
		if err := n.conn.DelRule(candidate); err != nil {
			return fmt.Errorf("delete stale lease rule %s handle %d: %w", leaseID, candidate.Handle, err)
		}
		removed++
	}
	if removed > 0 {
		if err := n.conn.Flush(); err != nil {
			return fmt.Errorf("flush stale lease rules %s: %w", leaseID, err)
		}
	}
	return nil
}

// removeStaleAddressRules cleans previous full-address DNAT allocations,
// including rules tagged with an older lease ID and legacy untagged rules.
// It only inspects this agent's dedicated wraplink prerouting chain.
func (n *NFT) removeStaleAddressRules(address net.IP) error {
	installed, err := n.conn.GetRules(n.table, n.chain)
	if err != nil {
		return fmt.Errorf("list nftables rules: %w", err)
	}
	removed := 0
	for _, candidate := range installed {
		if candidate.Handle == 0 || !isRouteDNATForAddress(candidate, address) {
			continue
		}
		if err := n.conn.DelRule(candidate); err != nil {
			return fmt.Errorf("delete old address rule handle %d: %w", candidate.Handle, err)
		}
		removed++
	}
	if removed > 0 {
		if err := n.conn.Flush(); err != nil {
			return fmt.Errorf("flush old address rules: %w", err)
		}
	}
	return nil
}

// isRouteDNATForAddress matches the IPv4 destination comparison followed
// by a destination NAT action, without relying on the changing origin IP.
func isRouteDNATForAddress(rule *nftables.Rule, address net.IP) bool {
	if rule == nil || address.To4() == nil {
		return false
	}
	matchedAddress, matchedNAT := false, false
	for i, expression := range rule.Exprs {
		if payload, ok := expression.(*expr.Payload); ok &&
			payload.Base == expr.PayloadBaseNetworkHeader &&
			payload.Offset == 16 && payload.Len == 4 && i+1 < len(rule.Exprs) {
			if comparison, ok := rule.Exprs[i+1].(*expr.Cmp); ok &&
				comparison.Op == expr.CmpOpEq &&
				bytes.Equal(comparison.Data, address.To4()) {
				matchedAddress = true
			}
		}
		if nat, ok := expression.(*expr.NAT); ok && nat.Type == expr.NATTypeDestNAT {
			matchedNAT = true
		}
	}
	return matchedAddress && matchedNAT
}

// removeOutputLeaseRules reconciles the local NAT chain using the same lease tags.
func (n *NFT) removeOutputLeaseRules(leaseID string, wanted []*nftables.Rule) error {
	installed, err := n.conn.GetRules(n.table, n.outputChain)
	if err != nil {
		return err
	}
	prefix := []byte("wraplink:lease:" + leaseID + ":")
	removed := 0
	for _, candidate := range installed {
		owned := bytes.HasPrefix(candidate.UserData, prefix)
		if !owned && len(candidate.UserData) == 0 {
			for _, rule := range wanted {
				if reflect.DeepEqual(rule.Exprs, candidate.Exprs) {
					owned = true
					break
				}
			}
		}
		if !owned || candidate.Handle == 0 {
			continue
		}
		if err := n.conn.DelRule(candidate); err != nil {
			return err
		}
		removed++
	}
	if removed > 0 {
		return n.conn.Flush()
	}
	return nil
}

func (n *NFT) removeStaleOutputAddressRules(address net.IP) error {
	installed, err := n.conn.GetRules(n.table, n.outputChain)
	if err != nil {
		return err
	}
	removed := 0
	for _, rule := range installed {
		if rule.Handle == 0 || !isRouteDNATForAddress(rule, address) {
			continue
		}
		if err := n.conn.DelRule(rule); err != nil {
			return err
		}
		removed++
	}
	if removed > 0 {
		return n.conn.Flush()
	}
	return nil
}

func (n *NFT) recoverOutputRouteHandles(leaseID string, rules []*nftables.Rule) error {
	installed, err := n.conn.GetRules(n.table, n.outputChain)
	if err != nil {
		return err
	}
	for _, wanted := range rules {
		var match *nftables.Rule
		for _, candidate := range installed {
			if bytes.Equal(wanted.UserData, candidate.UserData) {
				if match != nil {
					return fmt.Errorf("ambiguous output rule for lease %s", leaseID)
				}
				match = candidate
			}
		}
		if match == nil {
			return fmt.Errorf("missing output rule for lease %s", leaseID)
		}
		wanted.Handle = match.Handle
	}
	return nil
}

// recoverRouteHandles reconciles in-memory rules with kernel-assigned handles.
// Older rules have no lease metadata, so they are only recovered when the full
// expression sequence uniquely matches the rule we originally installed.
func (n *NFT) recoverRouteHandles(leaseID string, rules []*nftables.Rule) error {
	installed, err := n.conn.GetRules(n.table, n.chain)
	if err != nil {
		return fmt.Errorf("list nftables route rules: %w", err)
	}
	used := make(map[uint64]bool)
	for _, wanted := range rules {
		if wanted == nil {
			return fmt.Errorf("route %s has nil rule", leaseID)
		}
		var match *nftables.Rule
		for _, candidate := range installed {
			if candidate.Handle == 0 || used[candidate.Handle] {
				continue
			}
			taggedMatch := len(wanted.UserData) > 0 && bytes.Equal(wanted.UserData, candidate.UserData)
			legacyMatch := len(candidate.UserData) == 0 && reflect.DeepEqual(wanted.Exprs, candidate.Exprs)
			if taggedMatch || legacyMatch {
				if match != nil {
					return fmt.Errorf("ambiguous nftables rule for lease %s", leaseID)
				}
				match = candidate
			}
		}
		if match == nil {
			return fmt.Errorf("nftables rule for lease %s not found", leaseID)
		}
		wanted.Handle = match.Handle
		used[match.Handle] = true
	}
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
