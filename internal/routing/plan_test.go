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
	slug, _ := ParseWorkspaceSlug("dev")

	plan := BuildPlan("exchange-1", slug, route, nil)
	if len(plan) != 3 {
		t.Fatalf("plan length = %d", len(plan))
	}
	if plan[0].Tier != 0 || plan[1].Tier != 0 || plan[2].Tier != 1 {
		t.Fatalf("tier order = %d,%d,%d", plan[0].Tier, plan[1].Tier, plan[2].Tier)
	}
	for _, attempt := range plan {
		if attempt.Route != name || attempt.Target.ID().String() == "outside" {
			t.Fatalf("unexpected attempt: %+v", attempt)
		}
	}
}

func TestBuildPlanIsDeterministic(t *testing.T) {
	tier, _ := NewTier([]Target{testTarget(t, "a"), testTarget(t, "b"), testTarget(t, "c")})
	name, _ := ParseRouteName("chat")
	route, _ := NewRoute(name, []Tier{tier})
	slug, _ := ParseWorkspaceSlug("dev")
	first := BuildPlan("same", slug, route, nil)
	second := BuildPlan("same", slug, route, nil)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("plans differ: %#v != %#v", first, second)
	}
}

func TestBuildPlanBalancesFirstAttemptAcrossExchangeIDs(t *testing.T) {
	tier, _ := NewTier([]Target{testTarget(t, "a"), testTarget(t, "b"), testTarget(t, "c")})
	name, _ := ParseRouteName("chat")
	route, _ := NewRoute(name, []Tier{tier})
	slug, _ := ParseWorkspaceSlug("dev")
	counts := map[string]int{}
	for i := 0; i < 600; i++ {
		counts[BuildPlan(string(rune(i)), slug, route, nil)[0].Target.ID().String()]++
	}
	for id, count := range counts {
		if count < 140 || count > 260 {
			t.Fatalf("first-attempt count for %s = %d, want rough equal distribution", id, count)
		}
	}
}
