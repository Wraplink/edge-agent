package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wraplink/edge-agent/internal/allocator"
	"github.com/wraplink/edge-agent/internal/client"
)

type routeLifecycleDataplane struct {
	applied []client.Lease
	removed []string
	failApply bool
}
func (d *routeLifecycleDataplane) Apply(_ context.Context, l client.Lease) error {
	if d.failApply { return errors.New("dataplane unavailable") }
	d.applied = append(d.applied, l)
	return nil
}
func (d *routeLifecycleDataplane) Remove(_ context.Context, id string) error {
	d.removed = append(d.removed, id)
	return nil
}
func (d *routeLifecycleDataplane) ReplaceAccessGrants(context.Context, bool, []client.AccessGrant) error { return nil }

func TestFullRouteProvisionAckAndRestartWithdrawal(t *testing.T) {
	const address = "192.0.2.10"
	var reports []client.ReportRequest
	restarted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/agent/report":
			var report client.ReportRequest
			if err := json.NewDecoder(r.Body).Decode(&report); err != nil { t.Errorf("decode report: %v", err) }
			reports = append(reports, report)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/v1/agent/heartbeat":
			if restarted {
				_, _ = w.Write([]byte(`{"ok":true,"registered":false,"release_lease_ids":["lease-7"],"entitlement_enforced":true}`))
			} else {
				_, _ = w.Write([]byte(`{"ok":true,"registered":true,"entitlement_enforced":true}`))
			}
		case "/api/v1/agent/register":
			_, _ = w.Write([]byte(`{"ok":true,"agent":{"id":"edge-1"}}`))
		default:
			http.Error(w, "unexpected endpoint", http.StatusNotFound)
		}
	}))
	defer server.Close()

	dp := &routeLifecycleDataplane{}
	a := New("edge-1", "eu", 0, 0, 1, client.New(server.URL, ""), nil, allocator.NewRoute([]string{address}), dp)
	lease := client.Lease{ID:"lease-7", Domain:"game.example", Type:TypeRoute, Address:address, Generation:7, DestinationIP:"198.51.100.20", Protocol:"tcp_udp"}
	if err := a.processRouteLease(context.Background(), lease); err != nil { t.Fatalf("provision: %v", err) }
	if len(dp.applied) != 1 || dp.applied[0].Address != address { t.Fatalf("route not applied with assigned IP: %+v", dp.applied) }
	if len(reports) != 1 || !reports[0].Success || reports[0].Generation != 7 || reports[0].Address != address {
		t.Fatalf("invalid activation ACK: %+v", reports)
	}
	restarted = true
	if err := a.sendHeartbeat(context.Background()); err != nil { t.Fatalf("restart recovery: %v", err) }
	if len(dp.removed) != 1 || dp.removed[0] != lease.ID { t.Fatalf("old route not withdrawn: %v", dp.removed) }
	if len(a.activeLeaseIDs()) != 0 { t.Fatal("old lease remained active after restart") }
}

func TestFullRouteApplyFailureNeverAcknowledged(t *testing.T) {
	dp := &routeLifecycleDataplane{failApply:true}
	a := New("edge-1", "eu", 0, 0, 1, nil, nil, allocator.NewRoute([]string{"192.0.2.10"}), dp)
	err := a.processRouteLease(context.Background(), client.Lease{
		ID:"lease-7", Domain:"game.example", Type:TypeRoute, Address:"192.0.2.10",
		Generation:7, DestinationIP:"198.51.100.20", Protocol:"tcp_udp",
	})
	if err == nil { t.Fatal("expected dataplane failure") }
	if len(a.activeLeaseIDs()) != 0 { t.Fatal("failed route must not become active") }
}
