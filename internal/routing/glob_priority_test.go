package routing

import (
	"errors"
	"reflect"
	"testing"
)

func routeWithOneTarget(t *testing.T, pattern, id string) Route {
	t.Helper()
	name, err := ParseRouteName(pattern)
	if err != nil {
		t.Fatal(err)
	}
	tier, err := NewTier([]Target{testTarget(t, id)})
	if err != nil {
		t.Fatal(err)
	}
	route, err := NewRoute(name, []Tier{tier})
	if err != nil {
		t.Fatal(err)
	}
	return route
}

func routeNames(routes []Route) []string {
	names := make([]string, len(routes))
	for i, route := range routes {
		names[i] = route.Name().String()
	}
	return names
}

func TestGoPathGlobSyntaxAndPriority(t *testing.T) {
	for _, pattern := range []string{"coding", "claude-sonnet-*", "deepseek/*", "gpt-?-mini", "gpt-[456]*", `literal\*star`, "*"} {
		if _, err := ParseRouteName(pattern); err != nil {
			t.Errorf("valid Go glob %q rejected: %v", pattern, err)
		}
	}
	for _, pattern := range []string{"", "[", "[a-", `\`, "  coding", "coding ", "bad\nname"} {
		if _, err := ParseRouteName(pattern); err == nil {
			t.Errorf("bad glob %q accepted", pattern)
		}
	}
	slug, _ := ParseWorkspaceSlug("dev")
	routes := []Route{
		routeWithOneTarget(t, "claude-*-sonnet", "family"),
		routeWithOneTarget(t, "claude-4-*", "version"),
		routeWithOneTarget(t, "deepseek/*", "namespaced"),
		routeWithOneTarget(t, "*", "fallback"),
	}
	workspace, err := NewWorkspace(slug, routes)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ model, pattern string }{
		{"claude-4-sonnet", "claude-*-sonnet"},
		{"claude-4-opus", "claude-4-*"},
		{"deepseek/chat", "deepseek/*"},
		{"another-model", "*"},
	} {
		resolved, err := workspace.ResolveRoute(tc.model)
		if err != nil || resolved.Name().String() != tc.pattern {
			t.Errorf("%q => %v, %v, want %s", tc.model, resolved.Name(), err, tc.pattern)
		}
	}
	if _, err := workspace.ResolveRoute("unknown/nested"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bare * matched slash: %v", err)
	}

	config, _ := NewConfig([]Workspace{workspace})
	reordered, err := config.SetRouteOrder(slug, []RouteName{routes[1].Name(), routes[0].Name(), routes[2].Name(), routes[3].Name()})
	if err != nil {
		t.Fatal(err)
	}
	newWorkspace, _ := reordered.Workspace(slug)
	resolved, err := newWorkspace.ResolveRoute("claude-4-sonnet")
	if err != nil || resolved.Name() != routes[1].Name() {
		t.Fatalf("after reorder => %v, %v", resolved.Name(), err)
	}
	if got := routeNames(newWorkspace.Routes()); !reflect.DeepEqual(got, []string{"claude-4-*", "claude-*-sonnet", "deepseek/*", "*"}) {
		t.Fatalf("stored order %v", got)
	}
	oldWorkspace, _ := config.Workspace(slug)
	if got := routeNames(oldWorkspace.Routes()); !reflect.DeepEqual(got, []string{"claude-*-sonnet", "claude-4-*", "deepseek/*", "*"}) {
		t.Fatalf("edited source %v", got)
	}
}

func TestReorderMustBeExactPermutationAndPreserveMetadata(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("dev")
	routes := []Route{routeWithOneTarget(t, "chat", "a"), routeWithOneTarget(t, "*", "b")}
	workspace, _ := NewWorkspace(slug, routes)
	workspace = workspace.WithManagementIncarnation("incarnation").WithManagementRevision(19).WithConfigurationRevision("pub")
	config, _ := NewConfig([]Workspace{workspace})
	for _, raw := range [][]string{{"chat"}, {"chat", "chat"}, {"chat", "absent"}} {
		order := make([]RouteName, len(raw))
		for i, name := range raw {
			order[i], _ = ParseRouteName(name)
		}
		if _, err := config.SetRouteOrder(slug, order); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("invalid order %v => %v", raw, err)
		}
	}
	next, err := config.SetRouteOrder(slug, []RouteName{routes[1].Name(), routes[0].Name()})
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := next.Workspace(slug)
	if actual.ManagementIncarnation() != "incarnation" || actual.ManagementRevision() != 19 || actual.ConfigurationRevision() != "pub" {
		t.Fatalf("route order edit changed unrelated Workspace state")
	}
	if got := routeNames(actual.Routes()); !reflect.DeepEqual(got, []string{"*", "chat"}) {
		t.Fatalf("order %v", got)
	}
}

func TestNewRoutesInsertBeforeCatchAll(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("dev")
	fallback := routeWithOneTarget(t, "*", "fallback")
	ws, err := NewWorkspace(slug, []Route{fallback})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := NewConfig([]Workspace{ws})
	n, _ := ParseRouteName("coding")
	cfg, err = cfg.CreateRoute(slug, n, testTarget(t, "coding"))
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := cfg.Workspace(slug)
	if got := routeNames(actual.Routes()); !reflect.DeepEqual(got, []string{"coding", "*"}) {
		t.Fatalf("new route shadowed at creation: %v", got)
	}
}

func TestGlobModelCatalogIDsAreExactOnly(t *testing.T) {
	for _, test := range []struct {
		pattern, exact string
		visible        bool
	}{
		{"coding", "coding", true},
		{"deepseek/chat", "deepseek/chat", true},
		{`literal\*`, "literal*", true},
		{"claude-*", "", false},
		{"deepseek/*", "", false},
		{"*", "", false},
		{"gpt-[45]", "", false},
	} {
		name, err := ParseRouteName(test.pattern)
		if err != nil {
			t.Fatal(err)
		}
		model, visible := name.ExactModelID()
		if model != test.exact || visible != test.visible {
			t.Errorf("catalog %q: (%q, %v), want (%q, %v)", test.pattern, model, visible, test.exact, test.visible)
		}
	}
}

func TestLiteralDefaultUsesOrdinaryFirstMatchAndDeletion(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("dev")
	literal := routeWithOneTarget(t, "default", "literal")
	catchall := routeWithOneTarget(t, "*", "catchall")
	for _, routes := range [][]Route{{literal, catchall}, {catchall, literal}} {
		workspace, err := NewWorkspace(slug, routes)
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := workspace.ResolveRoute("default")
		if err != nil || resolved.Name() != routes[0].Name() {
			t.Fatal("literal default bypassed ordinary first-match priority")
		}
	}
	routes := []Route{routeWithOneTarget(t, "coding", "coding"), literal, catchall}
	for _, deleted := range routes {
		workspace, _ := NewWorkspace(slug, routes)
		config, _ := NewConfig([]Workspace{workspace})
		next, err := config.DeleteRoute(slug, deleted.Name())
		if err != nil {
			t.Fatalf("delete %s required a successor: %v", deleted.Name(), err)
		}
		readback, _ := next.Workspace(slug)
		if len(readback.Routes()) != 2 {
			t.Fatal("deletion changed sibling routes")
		}
		if _, exists := readback.Route(deleted.Name()); exists {
			t.Fatal("deleted route remains")
		}
	}
}
