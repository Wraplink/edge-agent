package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/wraplink/edge-agent/internal/allocator"
	"github.com/wraplink/edge-agent/internal/client"
	"github.com/wraplink/edge-agent/internal/dataplane"
)

const (
	TypeSNI   = "sni"
	TypeRoute = "route"
)

type Agent struct {
	id       string
	region   string
	interval time.Duration

	sniCapacity   int
	routeCapacity int

	dnsClient *client.Client

	sniAllocator   allocator.SNIAllocator
	routeAllocator allocator.RouteAllocator
	dataplane      *dataplane.Manager

	mu     sync.RWMutex
	active map[string]client.Lease
}

func New(
	id string,
	region string,
	interval time.Duration,
	sniCapacity int,
	routeCapacity int,
	dnsClient *client.Client,
	sniAllocator allocator.SNIAllocator,
	routeAllocator allocator.RouteAllocator,
	dataplaneManager *dataplane.Manager,
) *Agent {
	return &Agent{
		id:             id,
		region:         region,
		interval:       interval,
		sniCapacity:    sniCapacity,
		routeCapacity:  routeCapacity,
		dnsClient:      dnsClient,
		sniAllocator:   sniAllocator,
		routeAllocator: routeAllocator,
		dataplane:      dataplaneManager,
		active:         make(map[string]client.Lease),
	}
}

func (a *Agent) Run(ctx context.Context) error {
	log.Printf("agent: starting id=%s region=%s interval=%s", a.id, a.region, a.interval)

	if err := a.register(ctx); err != nil {
		return fmt.Errorf("register agent: %w", err)
	}

	if err := a.cycle(ctx); err != nil {
		log.Printf("agent: initial cycle failed: %v", err)
	}

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := a.cycle(ctx); err != nil {
				log.Printf("agent: cycle failed: %v", err)
			}
		}
	}
}

func (a *Agent) register(ctx context.Context) error {
	return a.dnsClient.Register(
		ctx,
		a.id,
		a.region,
		a.sniCapacity,
		a.routeCapacity,
	)
}

func (a *Agent) cycle(ctx context.Context) error {
	if err := a.sendHeartbeat(ctx); err != nil {
		log.Printf("agent: heartbeat/reconcile failed: %v", err)
	}

	sniAvailable, routeAvailable := a.availableCapacity()
	if sniAvailable <= 0 && routeAvailable <= 0 {
		return nil
	}

	response, err := a.dnsClient.Poll(ctx, a.id, sniAvailable, routeAvailable)
	if err != nil {
		return fmt.Errorf("poll dns-control: %w", err)
	}
	if len(response.Leases) == 0 {
		return nil
	}

	log.Printf("agent: received %d lease(s) revision=%d", len(response.Leases), response.Revision)
	for _, lease := range response.Leases {
		if err := a.processLease(ctx, lease); err != nil {
			log.Printf(
				"agent: process lease id=%s domain=%s type=%s: %v",
				lease.ID,
				lease.Domain,
				lease.Type,
				err,
			)
			if reportErr := a.reportFailure(ctx, lease); reportErr != nil {
				log.Printf("agent: report failure lease=%s: %v", lease.ID, reportErr)
			}
		}
	}
	return nil
}

func (a *Agent) processLease(ctx context.Context, lease client.Lease) error {
	switch lease.Type {
	case TypeSNI:
		return a.processSNILease(ctx, lease)
	case TypeRoute:
		return a.processRouteLease(ctx, lease)
	default:
		return fmt.Errorf("unsupported lease type %q", lease.Type)
	}
}

func (a *Agent) processSNILease(ctx context.Context, lease client.Lease) error {
	if a.countActive(TypeSNI) >= a.sniCapacity {
		return fmt.Errorf("SNI capacity exhausted")
	}

	result, err := a.sniAllocator.Allocate(ctx, lease.Domain)
	if err != nil {
		return fmt.Errorf("allocate SNI: %w", err)
	}

	report := client.ReportRequest{
		AgentID: a.id,
		LeaseID: lease.ID,
		Success: true,
		Address: result.Address,
		SNI:     result.SNI,
	}
	if err := a.dnsClient.Report(ctx, report); err != nil {
		if releaseErr := a.sniAllocator.Release(ctx, lease.Domain, result); releaseErr != nil {
			log.Printf("agent: release SNI after report failure domain=%s: %v", lease.Domain, releaseErr)
		}
		return fmt.Errorf("report SNI allocation: %w", err)
	}

	activeLease := lease
	activeLease.Address = result.Address
	activeLease.SNI = result.SNI

	a.mu.Lock()
	a.active[lease.ID] = activeLease
	a.mu.Unlock()

	log.Printf(
		"agent: SNI active domain=%s address=%s sni=%s lease=%s",
		activeLease.Domain,
		activeLease.Address,
		activeLease.SNI,
		activeLease.ID,
	)
	return nil
}

func (a *Agent) processRouteLease(ctx context.Context, lease client.Lease) error {
	if a.countActive(TypeRoute) >= a.routeCapacity {
		return fmt.Errorf("route capacity exhausted")
	}

	result, err := a.routeAllocator.Allocate(
		ctx,
		lease.Domain,
		lease.DestinationIP,
		lease.Protocol,
		lease.DestinationPortStart,
		lease.DestinationPortEnd,
	)
	if err != nil {
		return fmt.Errorf("allocate route: %w", err)
	}

	activeLease := lease
	activeLease.Address = result.Address
	activeLease.RouteID = result.RouteID
	activeLease.DestinationIP = result.DestinationIP
	activeLease.Protocol = result.Protocol
	activeLease.DestinationPortStart = result.DestinationPortStart
	activeLease.DestinationPortEnd = result.DestinationPortEnd

	if a.dataplane == nil {
		_ = a.routeAllocator.Release(ctx, lease.Domain, result)
		return fmt.Errorf("dataplane manager is nil")
	}
	if err := a.dataplane.Apply(ctx, activeLease); err != nil {
		_ = a.routeAllocator.Release(ctx, lease.Domain, result)
		return fmt.Errorf("apply route dataplane: %w", err)
	}

	report := client.ReportRequest{
		AgentID: a.id,
		LeaseID: lease.ID,
		Success: true,
		Address: activeLease.Address,
		RouteID: activeLease.RouteID,
	}
	if err := a.dnsClient.Report(ctx, report); err != nil {
		_ = a.dataplane.Remove(ctx, lease.ID)
		_ = a.routeAllocator.Release(ctx, lease.Domain, result)
		return fmt.Errorf("report route allocation: %w", err)
	}

	a.mu.Lock()
	a.active[lease.ID] = activeLease
	a.mu.Unlock()

	log.Printf(
		"agent: route active domain=%s address=%s destination=%s protocol=%s ports=%s route_id=%s lease=%s",
		activeLease.Domain,
		activeLease.Address,
		activeLease.DestinationIP,
		activeLease.Protocol,
		formatPortRange(activeLease.DestinationPortStart, activeLease.DestinationPortEnd),
		activeLease.RouteID,
		activeLease.ID,
	)
	return nil
}

func (a *Agent) reportFailure(ctx context.Context, lease client.Lease) error {
	return a.dnsClient.Report(ctx, client.ReportRequest{
		AgentID: a.id,
		LeaseID: lease.ID,
		Success: false,
	})
}

// sendHeartbeat is also the desired-state reconciliation step. It runs even
// with zero active leases so dns-control can track an idle edge as healthy.
func (a *Agent) sendHeartbeat(ctx context.Context) error {
	leaseIDs := a.activeLeaseIDs()

	response, err := a.dnsClient.Heartbeat(ctx, a.id, leaseIDs)
	if err != nil {
		return err
	}

	var releaseErrs []error
	for _, leaseID := range response.ReleaseLeaseIDs {
		if err := a.releaseLease(ctx, leaseID); err != nil {
			releaseErrs = append(releaseErrs, fmt.Errorf("release lease %s: %w", leaseID, err))
		}
	}

	if !response.Registered {
		if err := a.register(ctx); err != nil {
			releaseErrs = append(releaseErrs, fmt.Errorf("re-register agent: %w", err))
		} else {
			log.Printf("agent: re-registered after dns-control state reset")
		}
	}

	return errors.Join(releaseErrs...)
}

func (a *Agent) releaseLease(ctx context.Context, leaseID string) error {
	a.mu.RLock()
	lease, ok := a.active[leaseID]
	a.mu.RUnlock()
	if !ok {
		return nil
	}

	result := allocator.Result{
		Address:              lease.Address,
		SNI:                  lease.SNI,
		RouteID:              lease.RouteID,
		DestinationIP:        lease.DestinationIP,
		Protocol:             lease.Protocol,
		DestinationPortStart: lease.DestinationPortStart,
		DestinationPortEnd:   lease.DestinationPortEnd,
	}

	switch lease.Type {
	case TypeSNI:
		if err := a.sniAllocator.Release(ctx, lease.Domain, result); err != nil {
			return err
		}
	case TypeRoute:
		if a.dataplane == nil {
			return fmt.Errorf("dataplane manager is nil")
		}
		if err := a.dataplane.Remove(ctx, lease.ID); err != nil {
			return err
		}
		if err := a.routeAllocator.Release(ctx, lease.Domain, result); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported lease type %q", lease.Type)
	}

	a.mu.Lock()
	delete(a.active, leaseID)
	a.mu.Unlock()

	log.Printf("agent: released lease=%s domain=%s type=%s", lease.ID, lease.Domain, lease.Type)
	return nil
}

func (a *Agent) activeLeaseIDs() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	leaseIDs := make([]string, 0, len(a.active))
	for id := range a.active {
		leaseIDs = append(leaseIDs, id)
	}
	return leaseIDs
}

func (a *Agent) availableCapacity() (int, int) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	sniUsed := 0
	routeUsed := 0
	for _, lease := range a.active {
		switch lease.Type {
		case TypeSNI:
			sniUsed++
		case TypeRoute:
			routeUsed++
		}
	}

	sniAvailable := a.sniCapacity - sniUsed
	routeAvailable := a.routeCapacity - routeUsed
	if sniAvailable < 0 {
		sniAvailable = 0
	}
	if routeAvailable < 0 {
		routeAvailable = 0
	}
	return sniAvailable, routeAvailable
}

func (a *Agent) countActive(leaseType string) int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	count := 0
	for _, lease := range a.active {
		if lease.Type == leaseType {
			count++
		}
	}
	return count
}

func formatPortRange(start, end uint16) string {
	if start == 0 && end == 0 {
		return "all"
	}
	if start == end {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}
