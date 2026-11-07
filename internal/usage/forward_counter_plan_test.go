package usage

import (
	"github.com/wraplink/edge-agent/internal/client"
	"testing"
)

func TestPlanForwardCountersDeduplicatesSharedOrigin(t *testing.T) {
	leases := []client.Lease{
		{ID: "one", Type: "route", DestinationIP: "198.51.100.5", Protocol: "tcp_udp"},
		{ID: "two", Type: "route", DestinationIP: "198.51.100.5", Protocol: "tcp"},
	}
	rules, err := PlanForwardCounters(leases, []string{"192.0.2.1", "192.0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 4 {
		t.Fatalf("expected TCP/UDP upload/download once each, got %+v", rules)
	}
	for _, r := range rules {
		if r.Tag == "" || r.ClientIP != "192.0.2.1" || r.OriginIP != "198.51.100.5" {
			t.Fatalf("invalid rule %+v", r)
		}
	}
}

func TestPlanForwardCountersRejectsInvalid(t *testing.T) {
	if _, err := PlanForwardCounters(nil, []string{"bad"}); err == nil {
		t.Fatal("invalid client accepted")
	}
	if _, err := PlanForwardCounters([]client.Lease{{Type: "route", DestinationIP: "bad", Protocol: "tcp"}}, []string{"192.0.2.1"}); err == nil {
		t.Fatal("invalid origin accepted")
	}
}
