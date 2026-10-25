package workspaces

import "testing"

func TestRouteReadbackMatchesEffectiveTopologyAndRejectsChangedPriority(t *testing.T) {
	first := openAITarget("first")
	second := openAITarget("second")
	spec := RouteSpec{Tiers: []TierSpec{{Targets: []TargetDraft{first, second}}}}
	// Authoring/readback protocol normalization stays at the owning application.
	targets := []Target{}
	for _, draft := range []TargetDraft{second, first} {
		target, err := draft.routingTarget()
		if err != nil {
			t.Fatal(err)
		}
		targets = append(targets, projectTarget(target))
	}
	route := Route{Name: "chat", Tiers: []Tier{{Targets: targets}}}
	if !spec.MatchesRoute(route) {
		t.Fatal("equal-balance ordering changed semantics")
	}
	route.Tiers[0].Targets[0].Model = "changed"
	if spec.MatchesRoute(route) {
		t.Fatal("changed backend admitted")
	}
	route.Tiers[0].Targets[0] = route.Tiers[0].Targets[1]
	if spec.MatchesRoute(route) {
		t.Fatal("duplicate target admitted")
	}
	route.Tiers = []Tier{{Targets: targets[:1]}, {Targets: targets[1:]}}
	if spec.MatchesRoute(route) {
		t.Fatal("changed fallback priority admitted")
	}
	if (RouteSpec{}).MatchesRoute(Route{}) {
		t.Fatal("empty topology admitted")
	}
}
