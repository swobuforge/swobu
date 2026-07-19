package routing

import (
	"reflect"
	"testing"
)

func TestBuildPlanPreservesTierOrderAndRouteIsolation(t *testing.T) {
	primaryA := testTarget(t, "a")
	primaryB := testTarget(t, "b")
	fallback := testTarget(t, "c")
	primary, _ := NewTier([]Target{primaryA, primaryB})
	fallbackTier, _ := NewTier([]Target{fallback})
	name, _ := ParseRouteName("chat")
	route, _ := NewRoute(name, []Tier{primary, fallbackTier})
	plan := BuildPlan("exchange-1", route)
	if len(plan) != 3 {
		t.Fatalf("plan length = %d", len(plan))
	}
	if plan[2].ID() != fallback.ID() {
		t.Fatalf("fallback target = %q, want %q after primary tier", plan[2].ID(), fallback.ID())
	}
	for _, target := range plan {
		if target.ID().String() == "outside" {
			t.Fatalf("unexpected target: %+v", target)
		}
	}
}

func TestBuildPlanIsDeterministic(t *testing.T) {
	tier, _ := NewTier([]Target{testTarget(t, "a"), testTarget(t, "b"), testTarget(t, "c")})
	name, _ := ParseRouteName("chat")
	route, _ := NewRoute(name, []Tier{tier})
	first := BuildPlan("same", route)
	second := BuildPlan("same", route)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("plans differ: %#v != %#v", first, second)
	}
}

func TestBuildPlanBalancesFirstCandidateAcrossExchangeIDs(t *testing.T) {
	tier, _ := NewTier([]Target{testTarget(t, "a"), testTarget(t, "b"), testTarget(t, "c")})
	name, _ := ParseRouteName("chat")
	route, _ := NewRoute(name, []Tier{tier})
	counts := map[string]int{}
	for i := 0; i < 600; i++ {
		counts[BuildPlan(string(rune(i)), route)[0].ID().String()]++
	}
	for id, count := range counts {
		if count < 140 || count > 260 {
			t.Fatalf("first-candidate count for %s = %d, want rough equal distribution", id, count)
		}
	}
}
