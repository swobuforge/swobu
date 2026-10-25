package routing

import "testing"

func TestWorkspaceResolveRouteUsesExactModelNames(t *testing.T) {
	defaultName, _ := ParseRouteName("chat")
	firstName, _ := ParseRouteName("model-a")
	secondName, _ := ParseRouteName("model-b")
	defaultTier, _ := NewTier([]Target{testTarget(t, "default-target")})
	firstTier, _ := NewTier([]Target{testTarget(t, "memory-extraction")})
	secondTier, _ := NewTier([]Target{testTarget(t, "memory-consolidation")})
	defaultRoute, _ := NewRoute(defaultName, []Tier{defaultTier})
	firstRoute, _ := NewRoute(firstName, []Tier{firstTier})
	secondRoute, _ := NewRoute(secondName, []Tier{secondTier})
	slug, _ := ParseWorkspaceSlug("dev")
	workspace, err := NewWorkspace(slug, []Route{defaultRoute, firstRoute, secondRoute})
	if err != nil {
		t.Fatal(err)
	}

	for requested, wantUpstream := range map[string]string{
		"model-a": "upstream-memory-extraction",
		"model-b": "upstream-memory-consolidation",
	} {
		route, err := workspace.ResolveRoute(requested)
		if err != nil {
			t.Fatalf("ResolveRoute(%q): %v", requested, err)
		}
		target := route.Tiers()[0].Targets()[0]
		if got := target.Model().String(); got != wantUpstream {
			t.Fatalf("route %q target model = %q, want %q", requested, got, wantUpstream)
		}
	}
}
