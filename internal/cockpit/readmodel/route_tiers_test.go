package readmodel

import "testing"

func TestRouteReadModelUsesStructuralTiers(t *testing.T) {
	route := RouteReadModel{Enabled: true, Tiers: []TierReadModel{{Targets: []TargetReadModel{{ID: "a"}, {ID: "b"}}}, {Targets: []TargetReadModel{{ID: "c"}}}}}
	if route.TargetCount() != 3 || route.TierCount() != 2 || !route.HasBalancedTier() {
		t.Fatalf("route projection=%#v", route)
	}
	if got := route.RowValue(); got != "2 tiers · 3 targets" {
		t.Fatalf("RowValue=%q", got)
	}
	if tier, ok := route.TargetTier("c"); !ok || tier != 1 {
		t.Fatalf("TargetTier=%d,%v", tier, ok)
	}
}
