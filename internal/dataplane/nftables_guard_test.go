package dataplane

import (
	"bytes"
	"net"
	"testing"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
)

func TestGuardRulesDropUnauthorizedSource(t *testing.T) {
	for _, tc := range []struct {
		name       string
		local      bool
		wantChains int
	}{
		{name: "remote only", local: false, wantChains: 1},
		{name: "remote and local", local: true, wantChains: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := &NFT{
				table:              &nftables.Table{Name: tableName},
				guardPrerouting:    &nftables.Chain{Name: guardPreroutingName},
				guardOutput:        &nftables.Chain{Name: guardOutputName},
				entitledSet:        &nftables.Set{Name: entitledSetName},
				localOutputEnabled: tc.local,
			}
			address := net.ParseIP("192.0.2.10").To4()
			rules := n.makeGuardRules("lease-1", address)
			if len(rules) != tc.wantChains {
				t.Fatalf("got %d guards, want %d", len(rules), tc.wantChains)
			}
			for _, rule := range rules {
				if len(rule.Exprs) != 5 {
					t.Fatalf("got %d expressions, want 5", len(rule.Exprs))
				}
				dst, ok := rule.Exprs[0].(*expr.Payload)
				if !ok || dst.Base != expr.PayloadBaseNetworkHeader || dst.Offset != 16 || dst.Len != 4 {
					t.Fatalf("guard does not inspect destination IPv4: %#v", rule.Exprs[0])
				}
				cmp, ok := rule.Exprs[1].(*expr.Cmp)
				if !ok || cmp.Op != expr.CmpOpEq || !bytes.Equal(cmp.Data, address) {
					t.Fatalf("guard does not match allocated IP: %#v", rule.Exprs[1])
				}
				src, ok := rule.Exprs[2].(*expr.Payload)
				if !ok || src.Base != expr.PayloadBaseNetworkHeader || src.Offset != 12 || src.Len != 4 {
					t.Fatalf("guard does not inspect source IPv4: %#v", rule.Exprs[2])
				}
				lookup, ok := rule.Exprs[3].(*expr.Lookup)
				if !ok || !lookup.Invert || lookup.SetName != entitledSetName {
					t.Fatalf("guard must match sources absent from entitlement set: %#v", rule.Exprs[3])
				}
				verdict, ok := rule.Exprs[4].(*expr.Verdict)
				if !ok || verdict.Kind != expr.VerdictDrop {
					t.Fatalf("guard must drop unauthorized packets: %#v", rule.Exprs[4])
				}
			}
		})
	}
}
