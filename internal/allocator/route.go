package allocator

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
)

type Route struct {
	mu sync.Mutex

	addresses []string

	assigned map[string]Result
}

func NewRoute(addresses []string) *Route {
	cleaned := make([]string, 0, len(addresses))

	for _, address := range addresses {
		address = strings.TrimSpace(address)

		if address == "" {
			continue
		}

		cleaned = append(cleaned, address)
	}

	return &Route{
		addresses: cleaned,
		assigned:  make(map[string]Result),
	}
}

func (a *Route) Allocate(
	_ context.Context,
	domain string,
	destinationIP string,
	protocol string,
	destinationPortStart uint16,
	destinationPortEnd uint16,
) (Result, error) {
	domain = normalize(domain)
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	destinationIP = strings.TrimSpace(destinationIP)

	if domain == "" {
		return Result{}, fmt.Errorf("domain is empty")
	}

	if net.ParseIP(destinationIP) == nil {
		return Result{}, fmt.Errorf(
			"invalid destination IP %q",
			destinationIP,
		)
	}

	switch protocol {
	case "tcp", "udp", "tcp_udp":
	default:
		return Result{}, fmt.Errorf(
			"unsupported protocol %q",
			protocol,
		)
	}

	// 0/0 means all destination ports.
	if destinationPortStart == 0 &&
		destinationPortEnd == 0 {
		// Valid: all ports.
	} else {
		if destinationPortStart == 0 {
			return Result{}, fmt.Errorf(
				"destination port start cannot be 0 when a port range is specified",
			)
		}

		if destinationPortEnd == 0 {
			return Result{}, fmt.Errorf(
				"destination port end cannot be 0 when a port range is specified",
			)
		}

		if destinationPortStart > destinationPortEnd {
			return Result{}, fmt.Errorf(
				"invalid destination port range %d-%d",
				destinationPortStart,
				destinationPortEnd,
			)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	key := routeKey(
		domain,
		destinationIP,
		protocol,
		destinationPortStart,
		destinationPortEnd,
	)

	if result, ok := a.assigned[key]; ok {
		return result, nil
	}

	if len(a.addresses) == 0 {
		return Result{}, fmt.Errorf(
			"no edge addresses available",
		)
	}

	index := hash(key) % uint32(len(a.addresses))

	address := a.addresses[index]

	result := Result{
		Address:              address,
		RouteID:              "route-" + hashString(key),
		DestinationIP:        destinationIP,
		Protocol:             protocol,
		DestinationPortStart: destinationPortStart,
		DestinationPortEnd:   destinationPortEnd,
	}

	a.assigned[key] = result

	return result, nil
}

func (a *Route) Release(
	_ context.Context,
	domain string,
	result Result,
) error {
	domain = normalize(domain)

	key := routeKey(
		domain,
		result.DestinationIP,
		result.Protocol,
		result.DestinationPortStart,
		result.DestinationPortEnd,
	)

	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.assigned, key)

	return nil
}

func routeKey(
	domain string,
	destinationIP string,
	protocol string,
	destinationPortStart uint16,
	destinationPortEnd uint16,
) string {
	return fmt.Sprintf(
		"%s|%s|%s|%d|%d",
		domain,
		destinationIP,
		protocol,
		destinationPortStart,
		destinationPortEnd,
	)
}

// AllocateAssigned uses a control-plane selected edge IPv4 instead of hashing.
// The address must belong to this agent's configured public IPv4 inventory.
func (a *Route) AllocateAssigned(
	ctx context.Context, domain, destinationIP, protocol string,
	start, end uint16, assignedIP string,
) (Result, error) {
	assigned := net.ParseIP(strings.TrimSpace(assignedIP))
	if assigned == nil || assigned.To4() == nil {
		return Result{}, fmt.Errorf("invalid assigned edge IPv4 %q", assignedIP)
	}
	assignedIP = assigned.To4().String()
	allowed := false
	for _, address := range a.addresses {
		if address == assignedIP {
			allowed = true
			break
		}
	}
	if !allowed {
		return Result{}, fmt.Errorf("assigned edge IPv4 %s is not configured on this agent", assignedIP)
	}
	result, err := a.Allocate(ctx, domain, destinationIP, protocol, start, end)
	if err != nil {
		return Result{}, err
	}
	key := routeKey(normalize(domain), strings.TrimSpace(destinationIP), strings.ToLower(strings.TrimSpace(protocol)), start, end)
	a.mu.Lock()
	defer a.mu.Unlock()
	result.Address = assignedIP
	a.assigned[key] = result
	return result, nil
}
