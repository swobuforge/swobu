package routing

import (
	"errors"
	"reflect"
	"testing"
)

func targetVersions(route Route) map[string]TargetVersion {
	out := map[string]TargetVersion{}
	for _, tier := range route.Tiers() {
		for _, target := range tier.Targets() {
			out[target.ID().String()] = target.Version()
		}
	}
	return out
}

func routeWithTargets(t *testing.T, tiers ...[]string) Route {
	t.Helper()
	name, _ := ParseRouteName("chat")
	materialized := make([]Tier, len(tiers))
	for i, ids := range tiers {
		targets := make([]Target, len(ids))
		for j, id := range ids {
			targets[j] = testTarget(t, id)
		}
		materialized[i], _ = NewTier(targets)
	}
	route, err := NewRoute(name, materialized)
	if err != nil {
		t.Fatal(err)
	}
	return route
}

func specTopology(source Route, tiers ...[]string) RouteSpec {
	byID := map[string]TargetSpec{}
	for _, tier := range source.Spec().Tiers {
		for _, target := range tier.Targets {
			byID[target.ID.String()] = target
		}
	}
	out := RouteSpec{Tiers: make([]TierSpec, len(tiers))}
	for i, ids := range tiers {
		for _, id := range ids {
			out.Tiers[i].Targets = append(out.Tiers[i].Targets, byID[id])
		}
	}
	return out
}

func TestApplyRouteSpecPreservesVersionsForEveryTopologyOnlyTransition(t *testing.T) {
	current := routeWithTargets(t, []string{"a", "b"}, []string{"c"}, []string{"d"})
	wantVersions := targetVersions(current)
	cases := map[string][][]string{
		"unchanged":                         {{"a", "b"}, {"c"}, {"d"}},
		"fallback to primary singleton":     {{"c"}, {"a", "b"}, {"d"}},
		"primary to final fallback":         {{"b"}, {"c"}, {"d"}, {"a"}},
		"singleton joins balanced tier":     {{"a", "b", "c"}, {"d"}},
		"balanced member becomes singleton": {{"a"}, {"b"}, {"c"}, {"d"}},
		"balanced peers reorder":            {{"b", "a"}, {"c"}, {"d"}},
		"fallback tiers reorder":            {{"a", "b"}, {"d"}, {"c"}},
		"multiple topology changes":         {{"d", "b"}, {"c", "a"}},
	}
	for name, topology := range cases {
		t.Run(name, func(t *testing.T) {
			next, err := ApplyRouteSpec(current, specTopology(current, topology...))
			if err != nil {
				t.Fatal(err)
			}
			if got := targetVersions(next); !reflect.DeepEqual(got, wantVersions) {
				t.Fatalf("versions = %#v, want %#v", got, wantVersions)
			}
		})
	}
}

func TestApplyRouteSpecBackendIdentityMembersIncrementExactlyOnce(t *testing.T) {
	current := routeWithTargets(t, []string{"a"})
	base := current.Spec()
	baseTarget := base.Tiers[0].Targets[0]
	replacement := testTarget(t, "replacement")
	changedConnection, err := NewStandardConnection(baseTarget.Settings.Connection.Provider(), "", "env:OTHER_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*TargetSpec){
		"model": func(s *TargetSpec) { s.Settings.Model = replacement.Model() },
		"protocol": func(s *TargetSpec) {
			provider := s.Settings.Connection.Provider()
			s.Settings.Protocol, _ = ParseProtocol("chat_completions", provider, func(Provider, string) bool { return true })
		},
		"connection": func(s *TargetSpec) { s.Settings.Connection = changedConnection },
		"multiple fields": func(s *TargetSpec) {
			s.Settings.Model = replacement.Model()
			s.Settings.Connection = changedConnection
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			desired := base
			desired.Tiers = []TierSpec{{Targets: []TargetSpec{baseTarget}}}
			change(&desired.Tiers[0].Targets[0])
			next, err := ApplyRouteSpec(current, desired)
			if err != nil {
				t.Fatal(err)
			}
			if got := next.Tiers()[0].Targets()[0].Version(); got != initialTargetVersion+1 {
				t.Fatalf("version = %d, want %d", got, initialTargetVersion+1)
			}
		})
	}
}

func TestApplyRouteSpecAddsRemovesAndCombinesChangesAtomically(t *testing.T) {
	current := routeWithTargets(t, []string{"a"}, []string{"b"})
	desired := specTopology(current, []string{"b"})
	newTarget := testTarget(t, "c")
	desired.Tiers[0].Targets[0].Settings.Model = testTarget(t, "changed").Model()
	desired.Tiers = append(desired.Tiers, TierSpec{Targets: []TargetSpec{{
		ID: newTarget.ID(), Settings: TargetSettings{newTarget.Model(), newTarget.Protocol(), newTarget.Connection()},
	}}})
	next, err := ApplyRouteSpec(current, desired)
	if err != nil {
		t.Fatal(err)
	}
	versions := targetVersions(next)
	if _, exists := versions["a"]; exists || versions["b"] != 2 || versions["c"] != initialTargetVersion {
		t.Fatalf("versions after mixed change = %#v", versions)
	}
}

func TestApplyRouteSpecRejectsEveryStructuralInvalidityWithoutChangingSource(t *testing.T) {
	current := routeWithTargets(t, []string{"a"}, []string{"b"})
	valid := current.Spec()
	invalidTarget := valid.Tiers[0].Targets[0]
	invalidTarget.Settings.Model = UpstreamModel{}
	cases := map[string]RouteSpec{
		"no tiers":               {},
		"empty tier":             {Tiers: []TierSpec{{}}},
		"duplicate in tier":      {Tiers: []TierSpec{{Targets: []TargetSpec{valid.Tiers[0].Targets[0], valid.Tiers[0].Targets[0]}}}},
		"duplicate across tiers": {Tiers: []TierSpec{{Targets: valid.Tiers[0].Targets}, {Targets: valid.Tiers[0].Targets}}},
		"missing ID":             {Tiers: []TierSpec{{Targets: []TargetSpec{{Settings: valid.Tiers[0].Targets[0].Settings}}}}},
		"invalid target":         {Tiers: []TierSpec{{Targets: []TargetSpec{invalidTarget}}}},
	}
	want := current.Spec()
	for name, desired := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ApplyRouteSpec(current, desired); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error = %v, want invalid config", err)
			}
			if got := current.Spec(); !reflect.DeepEqual(got, want) {
				t.Fatalf("source mutated: %#v", got)
			}
		})
	}
}

func TestApplyRouteSpecRejectsVersionOverflow(t *testing.T) {
	current := routeWithTargets(t, []string{"a"})
	current.tiers[0].targets[0].version = ^TargetVersion(0)
	desired := current.Spec()
	desired.Tiers[0].Targets[0].Settings.Model = testTarget(t, "changed").Model()
	if _, err := ApplyRouteSpec(current, desired); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want invalid config", err)
	}
}

func TestConfigApplyRouteSpecNeverReusesGenerationAfterDelete(t *testing.T) {
	config := testConfig(t, testTarget(t, "x"), testTarget(t, "keep"))
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	workspace, _ := config.Workspace(slug)
	current, _ := workspace.Route(routeName)
	keepOnly := specTopology(current, []string{"keep"})

	withoutX, err := config.ApplyRouteSpec(slug, routeName, keepOnly)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ = withoutX.Workspace(slug)
	if got := workspace.TargetGenerations()[current.tiers[0].targets[0].id]; got != 1 {
		t.Fatalf("retained generation = %d, want 1", got)
	}

	readded := keepOnly
	x := current.Spec().Tiers[0].Targets[0]
	readded.Tiers = append(readded.Tiers, TierSpec{Targets: []TargetSpec{x}})
	withXAgain, err := withoutX.ApplyRouteSpec(slug, routeName, readded)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ = withXAgain.Workspace(slug)
	route, _ := workspace.Route(routeName)
	if got := targetVersions(route)["x"]; got != 2 {
		t.Fatalf("re-added target generation = %d, want 2", got)
	}

	withoutXAgain, err := withXAgain.ApplyRouteSpec(slug, routeName, keepOnly)
	if err != nil {
		t.Fatal(err)
	}
	withXThird, err := withoutXAgain.ApplyRouteSpec(slug, routeName, readded)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ = withXThird.Workspace(slug)
	route, _ = workspace.Route(routeName)
	if got := targetVersions(route)["x"]; got != 3 {
		t.Fatalf("second re-add generation = %d, want 3", got)
	}
}

func TestConfigApplyRouteSpecUsesRetainedGenerationForDifferentBackend(t *testing.T) {
	config := testConfig(t, testTarget(t, "x"), testTarget(t, "keep"))
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	workspace, _ := config.Workspace(slug)
	current, _ := workspace.Route(routeName)
	keepOnly := specTopology(current, []string{"keep"})
	withoutX, err := config.ApplyRouteSpec(slug, routeName, keepOnly)
	if err != nil {
		t.Fatal(err)
	}

	changed := current.Spec().Tiers[0].Targets[0]
	changed.Settings.Model = testTarget(t, "different-backend").Model()
	desired := keepOnly
	desired.Tiers = append(desired.Tiers, TierSpec{Targets: []TargetSpec{changed}})
	next, err := withoutX.ApplyRouteSpec(slug, routeName, desired)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ = next.Workspace(slug)
	route, _ := workspace.Route(routeName)
	if got := targetVersions(route)["x"]; got != 2 {
		t.Fatalf("different backend re-add generation = %d, want 2", got)
	}
}

func TestConfigApplyRouteSpecRejectsExhaustedAbsentGenerationAtomically(t *testing.T) {
	config := testConfig(t, testTarget(t, "keep"))
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	workspace, _ := config.Workspace(slug)
	x := testTarget(t, "x")
	workspace.generations[x.id] = ^TargetVersion(0)
	config.workspaces[slug] = workspace
	current, _ := workspace.Route(routeName)
	desired := current.Spec()
	desired.Tiers = append(desired.Tiers, TierSpec{Targets: []TargetSpec{{ID: x.id, Settings: TargetSettings{Model: x.model, Protocol: x.protocol, Connection: x.connection}}}})

	if _, err := config.ApplyRouteSpec(slug, routeName, desired); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want invalid config", err)
	}
	unchanged, _ := config.Workspace(slug)
	if _, found := unchanged.Route(routeName); !found || unchanged.TargetGenerations()[x.id] != ^TargetVersion(0) {
		t.Fatalf("source config mutated: %#v", unchanged)
	}
}
