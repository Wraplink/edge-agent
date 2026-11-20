package agent

import (
 "testing"

 "github.com/wraplink/edge-agent/internal/inventory"
)

func TestRouteReadiness(t *testing.T) {
 tests := []struct {
  name string
  agent Agent
  wantReady bool
 }{
  {"disabled", Agent{routeCapacity: 0, routeInventory: &inventory.Client{}, routePublicIPs: []string{"192.0.2.1"}, dataplane: &fakeDataplane{}}, false},
  {"missing inventory", Agent{routeCapacity: 1, routePublicIPs: []string{"192.0.2.1"}, dataplane: &fakeDataplane{}}, false},
  {"missing public IP", Agent{routeCapacity: 1, routeInventory: &inventory.Client{}, dataplane: &fakeDataplane{}}, false},
  {"missing dataplane", Agent{routeCapacity: 1, routeInventory: &inventory.Client{}, routePublicIPs: []string{"192.0.2.1"}}, false},
  {"prerequisites present", Agent{routeCapacity: 1, routeInventory: &inventory.Client{}, routePublicIPs: []string{"192.0.2.1"}, dataplane: &fakeDataplane{}}, true},
 }
 for _, tt := range tests {
  t.Run(tt.name, func(t *testing.T) {
   got := tt.agent.routeReadiness()
   if got.ReadyForHealthProbe != tt.wantReady {
    t.Fatalf("ready for health probe = %t, want %t", got.ReadyForHealthProbe, tt.wantReady)
   }
  })
 }
}
