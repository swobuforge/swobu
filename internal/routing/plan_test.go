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

func TestBuildPlanIgnoresConfigurationOrderWithinTier(t *testing.T) {
	a, b, c := testTarget(t, "a"), testTarget(t, "b"), testTarget(t, "c")
	firstTier, _ := NewTier([]Target{a, b, c})
	secondTier, _ := NewTier([]Target{c, a, b})
	name, _ := ParseRouteName("chat")
	firstRoute, _ := NewRoute(name, []Tier{firstTier})
	secondRoute, _ := NewRoute(name, []Tier{secondTier})
	first := BuildPlan("placement-key", firstRoute)
	second := BuildPlan("placement-key", secondRoute)
	for index := range first {
		if first[index].ID() != second[index].ID() {
			t.Fatalf("plans differ at %d: %s != %s", index, first[index].ID(), second[index].ID())
		}
	}
}

func TestBuildPlanSeparatesRouteNames(t *testing.T) {
	targets := []Target{testTarget(t, "a"), testTarget(t, "b"), testTarget(t, "c"), testTarget(t, "d")}
	tier, _ := NewTier(targets)
	firstName, _ := ParseRouteName("first")
	secondName, _ := ParseRouteName("second")
	firstRoute, _ := NewRoute(firstName, []Tier{tier})
	secondRoute, _ := NewRoute(secondName, []Tier{tier})
	if reflect.DeepEqual(BuildPlan("placement-key", firstRoute), BuildPlan("placement-key", secondRoute)) {
		t.Fatal("different route names unexpectedly produced an identical plan")
	}
}

func TestCapturedPlanIsUnaffectedByLaterRouteReplacement(t *testing.T) {
	current := routeWithTargets(t, []string{"a"}, []string{"b"}, []string{"c"})
	captured := BuildPlan("exchange", current)
	desired := specTopology(current, []string{"c"}, []string{"a"}, []string{"b"})
	next, err := ApplyRouteSpec(current, desired)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{captured[0].ID().String(), captured[1].ID().String(), captured[2].ID().String()}; !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("captured plan changed = %#v", got)
	}
	fresh := BuildPlan("exchange", next)
	if got := []string{fresh[0].ID().String(), fresh[1].ID().String(), fresh[2].ID().String()}; !reflect.DeepEqual(got, []string{"c", "a", "b"}) {
		t.Fatalf("fresh plan = %#v", got)
	}
}
