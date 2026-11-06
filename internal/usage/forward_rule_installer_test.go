package usage

import (
 "strings"
 "testing"
 "github.com/wraplink/edge-agent/internal/client"
)

func TestRenderForwardRulesConntrackScope(t *testing.T) {
 leases:=[]client.Lease{{ID:"r1",Type:"route",Address:"203.0.113.7",DestinationIP:"198.51.100.5",Protocol:"tcp_udp"}}
 script,err:=RenderForwardRules(leases,[]string{"192.0.2.10"},"wraplink","route_accounting")
 if err!=nil {t.Fatal(err)}
 if !strings.HasPrefix(script,"flush chain ip wraplink route_accounting\n") {t.Fatal(script)}
 if strings.Count(script," counter comment ")!=4 {t.Fatalf("expected four counters: %s",script)}
 for _,part:=range []string{"ct original daddr 203.0.113.7","ct original saddr 192.0.2.10","ip daddr 198.51.100.5","ip saddr 198.51.100.5","meta l4proto tcp","meta l4proto udp"} {
  if !strings.Contains(script,part) {t.Fatalf("missing %q in %s",part,script)}
 }
}

func TestRenderForwardRulesRejectsUnsafeIdentifiers(t *testing.T) {
 if _,err:=RenderForwardRules(nil,nil,"wraplink;delete table","chain");err==nil {t.Fatal("accepted command injection")}
 if _,err:=RenderForwardRules([]client.Lease{{Type:"route",Address:"bad",DestinationIP:"198.51.100.5",Protocol:"tcp"}},nil,"","");err==nil {t.Fatal("accepted invalid route IP")}
}
