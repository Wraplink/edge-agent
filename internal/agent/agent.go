package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/wraplink/edge-agent/internal/allocator"
	"github.com/wraplink/edge-agent/internal/client"
)

const (
	TypeSNI   = "sni"
	TypeRoute = "route"
)

type Dataplane interface {
	Apply(context.Context, client.Lease) error
	Remove(context.Context, string) error
	ReplaceAccessGrants(context.Context, bool, []client.AccessGrant) error
}

type Agent struct {
	id       string
	region   string
	interval time.Duration

	sniCapacity   int
	routeCapacity int

	dnsClient *client.Client

	sniAllocator   allocator.SNIAllocator
	routeAllocator allocator.RouteAllocator
	dataplane      Dataplane

	mu sync.RWMutex

	active map[string]client.Lease

	entitlementConfigured bool
	entitlementEnforced   bool
	entitlementRevision   uint64
	entitlements          map[string]client.AccessGrant
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
	dataplaneManager Dataplane,
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
		entitlements:   make(map[string]client.AccessGrant),
	}
}

func (a *Agent) Run(ctx context.Context) error {
	log.Printf("agent: starting id=%s region=%s interval=%s", a.id, a.region, a.interval)

	// Deny access until the first authenticated entitlement snapshot arrives.
	if err := a.replaceEntitlements(ctx, true, 0, nil); err != nil {
		return fmt.Errorf("initialize fail-closed edge access: %w", err)
	}

	// DNS-control can restart or briefly return 5xx while initializing.
	// Stay idle and retry transient errors, but fail fast for invalid
	// credentials or bad configuration (HTTP 4xx except 429).
	if err := a.registerWithRetry(ctx, 2*time.Second, 30*time.Second); err != nil {
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

// registerWithRetry does not proceed to polling/allocations until dns-control
// confirms registration. A persistent 500 keeps the process running and logs
// the server response so the actual upstream issue can be investigated.
func (a *Agent) registerWithRetry(ctx context.Context, initialDelay, maxDelay time.Duration) error {
	if initialDelay <= 0 {
		initialDelay = 2 * time.Second
	}
	if maxDelay < initialDelay {
		maxDelay = initialDelay
	}

	delay := initialDelay
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		attempt++
		err := a.register(ctx)
		if err == nil {
			if attempt > 1 {
				log.Printf("agent: registration recovered after %d attempts", attempt)
			}
			return nil
		}

		var statusErr *client.HTTPStatusError
		if errors.As(err, &statusErr) {
			if statusErr.StatusCode != http.StatusTooManyRequests && statusErr.StatusCode < 500 {
				return fmt.Errorf("permanent registration failure: %w", err)
			}
		} else if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("agent: registration attempt %d failed; retry in %s: %v", attempt, delay, err)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		if delay < maxDelay {
			if delay > maxDelay/2 {
				delay = maxDelay
			} else {
				delay *= 2
			}
		}
	}
}

func (a *Agent) cycle(ctx context.Context) error {
	if err := a.sendHeartbeat(ctx); err != nil {
		log.Printf("agent: heartbeat/reconcile failed: %v", err)
		// A disconnected control plane cannot extend stale authorizations.
		// Clear the kernel allowlist rather than retaining revoked clients.
		if revokeErr := a.replaceEntitlements(ctx, true, 0, nil); revokeErr != nil {
			return fmt.Errorf("heartbeat failed: %w; fail-closed revoke failed: %v", err, revokeErr)
		}
		return fmt.Errorf("heartbeat failed; access revoked: %w", err)
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
		return fmt.Errorf("SNI listener and source-IP enforcement are not configured for lease %s", lease.ID)
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
	entitlementRevision := a.currentEntitlementRevision()

	response, err := a.dnsClient.Heartbeat(
		ctx,
		a.id,
		leaseIDs,
		entitlementRevision,
	)
	if err != nil {
		return err
	}

	var reconcileErrs []error

	if !response.EntitlementEnforced {
		if err := a.replaceEntitlements(ctx, true, 0, nil); err != nil {
			return fmt.Errorf("disable unsafe access: %w", err)
		}
		return fmt.Errorf("dns-control did not enable entitlement enforcement")
	}

	if a.entitlementSyncRequired(response) {
		if err := a.replaceEntitlements(
			ctx,
			response.EntitlementEnforced,
			response.EntitlementRevision,
			response.Entitlements,
		); err != nil {
			reconcileErrs = append(reconcileErrs, err)
		}
	}

	for _, leaseID := range response.ReleaseLeaseIDs {
		if err := a.releaseLease(ctx, leaseID); err != nil {
			reconcileErrs = append(reconcileErrs, fmt.Errorf("release lease %s: %w", leaseID, err))
		}
	}

	if !response.Registered {
		if err := a.register(ctx); err != nil {
			reconcileErrs = append(reconcileErrs, fmt.Errorf("re-register agent: %w", err))
		} else {
			log.Printf("agent: re-registered after dns-control state reset")
		}
	}

	return errors.Join(reconcileErrs...)
}

func (a *Agent) entitlementSyncRequired(response client.HeartbeatResponse) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return !a.entitlementConfigured ||
		a.entitlementEnforced != response.EntitlementEnforced ||
		a.entitlementRevision != response.EntitlementRevision
}

func (a *Agent) replaceEntitlements(
	ctx context.Context,
	enforced bool,
	revision uint64,
	grants []client.AccessGrant,
) error {
	if a.dataplane == nil {
		return fmt.Errorf("dataplane manager is nil")
	}

	if err := a.dataplane.ReplaceAccessGrants(ctx, enforced, grants); err != nil {
		return fmt.Errorf("apply entitlement revision %d: %w", revision, err)
	}

	items := make(map[string]client.AccessGrant, len(grants))
	for _, grant := range grants {
		items[grant.ClientIP] = grant
	}

	a.mu.Lock()
	a.entitlementConfigured = true
	a.entitlementEnforced = enforced
	a.entitlementRevision = revision
	a.entitlements = items
	a.mu.Unlock()

	log.Printf(
		"agent: entitlement access synchronized enforced=%t revision=%d clients=%d",
		enforced,
		revision,
		len(items),
	)
	return nil
}

func (a *Agent) currentEntitlementRevision() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.entitlementRevision
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
