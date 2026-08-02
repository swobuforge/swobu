package routing

import (
	"errors"
	"strings"
	"testing"
)

func testProtocol(t *testing.T, provider Provider, raw string) Protocol {
	t.Helper()
	protocol, err := ParseProtocol(raw, provider, func(got Provider, protocol string) bool {
		return got == provider && protocol != "unsupported"
	})
	if err != nil {
		t.Fatal(err)
	}
	return protocol
}

func testTarget(t *testing.T, id string) Target {
	t.Helper()
	targetID, _ := ParseTargetID(id)
	model, _ := ParseUpstreamModel("upstream-" + id)
	connection, err := NewOpenAIConnection("env:OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewTarget(targetID, model, testProtocol(t, ProviderOpenAI, "responses"), connection)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func testConfig(t *testing.T, targets ...Target) Config {
	t.Helper()
	slug, _ := ParseWorkspaceSlug("dev")
	name, _ := ParseRouteName("chat")
	tier, err := NewTier(targets)
	if err != nil {
		t.Fatal(err)
	}
	route, err := NewRoute(name, []Tier{tier})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := NewWorkspace(slug, name, []Route{route})
	if err != nil {
		t.Fatal(err)
	}
	config, err := NewConfig([]Workspace{workspace})
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func TestConfigAllowsTargetIDsToRepeatAcrossWorkspaces(t *testing.T) {
	target := testTarget(t, "same")
	var workspaces []Workspace
	for _, rawSlug := range []string{"one", "two"} {
		slug, _ := ParseWorkspaceSlug(rawSlug)
		name, _ := ParseRouteName("route-" + rawSlug)
		tier, _ := NewTier([]Target{target})
		route, _ := NewRoute(name, []Tier{tier})
		workspace, _ := NewWorkspace(slug, name, []Route{route})
		workspaces = append(workspaces, workspace)
	}
	if _, err := NewConfig(workspaces); err != nil {
		t.Fatalf("NewConfig rejected workspace-scoped target IDs: %v", err)
	}
}

func TestWorkspaceRejectsDuplicateTargetIDsAcrossRoutes(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("dev")
	firstName, _ := ParseRouteName("one")
	secondName, _ := ParseRouteName("two")
	target := testTarget(t, "same")
	tier, _ := NewTier([]Target{target})
	first, _ := NewRoute(firstName, []Tier{tier})
	second, _ := NewRoute(secondName, []Tier{tier})
	if _, err := NewWorkspace(slug, firstName, []Route{first, second}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewWorkspace error = %v, want duplicate target rejection", err)
	}
}

func TestCommandIdentifiersUseURLSegmentGrammar(t *testing.T) {
	valid := []string{"a", "Model.Name-1_v2", strings.Repeat("x", 128)}
	invalid := []string{"", ".", "..", "foo/bar", "a?b", "a b", " padded", strings.Repeat("x", 129)}
	for _, raw := range valid {
		if _, err := ParseRouteName(raw); err != nil {
			t.Errorf("ParseRouteName(%q): %v", raw, err)
		}
		if _, err := ParseTargetID(raw); err != nil {
			t.Errorf("ParseTargetID(%q): %v", raw, err)
		}
	}
	for _, raw := range invalid {
		if _, err := ParseRouteName(raw); err == nil {
			t.Errorf("ParseRouteName(%q) unexpectedly succeeded", raw)
		}
		if _, err := ParseTargetID(raw); err == nil {
			t.Errorf("ParseTargetID(%q) unexpectedly succeeded", raw)
		}
	}
	if _, err := ParseRouteName("swobu"); err != nil {
		t.Errorf("ParseRouteName(\"swobu\"): %v", err)
	}
	if _, err := ParseRouteName(PublicDefaultRouteID); err == nil {
		t.Errorf("ParseRouteName(%q) unexpectedly accepted the reserved default-model ID", PublicDefaultRouteID)
	}
	if _, err := ParseTargetID(PublicDefaultRouteID); err != nil {
		t.Errorf("ParseTargetID(%q): %v", PublicDefaultRouteID, err)
	}
}

func TestWorkspaceResolveRouteUsesExactNameThenConfiguredDefault(t *testing.T) {
	defaultName, _ := ParseRouteName("chat")
	exactName, _ := ParseRouteName("exact")
	defaultTier, _ := NewTier([]Target{testTarget(t, "default-target")})
	exactTier, _ := NewTier([]Target{testTarget(t, "exact-target")})
	defaultRoute, _ := NewRoute(defaultName, []Tier{defaultTier})
	exactRoute, _ := NewRoute(exactName, []Tier{exactTier})
	slug, _ := ParseWorkspaceSlug("dev")
	workspace, err := NewWorkspace(slug, defaultName, []Route{defaultRoute, exactRoute})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		requested string
		want      string
	}{
		{requested: "exact", want: "exact"},
		{requested: PublicDefaultRouteID, want: "chat"},
		{requested: "fixed-client-model", want: "chat"},
		{requested: "provider/model:not-a-route", want: "chat"},
	} {
		route, err := workspace.ResolveRoute(test.requested)
		if err != nil || route.Name().String() != test.want {
			t.Fatalf("ResolveRoute(%q) = %q, %v, want %q", test.requested, route.Name().String(), err, test.want)
		}
	}
	for _, requested := range []string{"", " \t"} {
		if _, err := workspace.ResolveRoute(requested); !errors.Is(err, ErrEmptyRequestedRoute) {
			t.Fatalf("ResolveRoute(%q) error = %v, want ErrEmptyRequestedRoute", requested, err)
		}
	}
}

func TestCollectionsAreCloned(t *testing.T) {
	config := testConfig(t, testTarget(t, "a"))
	workspaces := config.Workspaces()
	workspaces[0] = Workspace{}
	if config.WorkspaceCount() != 1 {
		t.Fatal("mutating returned workspace slice changed config")
	}
	slug, _ := ParseWorkspaceSlug("dev")
	workspace, _ := config.Workspace(slug)
	routes := workspace.Routes()
	routes[0] = Route{}
	name, _ := ParseRouteName("chat")
	if _, ok := workspace.Route(name); !ok {
		t.Fatal("mutating returned route slice changed workspace")
	}
}

func TestConfigCloneOwnsNestedTargetStorage(t *testing.T) {
	original := testConfig(t, testTarget(t, "a"))
	clone := original.Clone()
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	id, _ := ParseTargetID("a")
	replacement := testTarget(t, "replacement")

	if _, err := clone.UpdateTargetSettings(slug, routeName, id, TargetSettings{
		Model: replacement.Model(), Protocol: replacement.Protocol(), Connection: replacement.Connection(),
	}); err != nil {
		t.Fatal(err)
	}

	workspace, _ := original.Workspace(slug)
	route, _ := workspace.Route(routeName)
	if got := route.Tiers()[0].Targets()[0].Model().String(); got != "upstream-a" {
		t.Fatalf("original model = %q after editing clone, want upstream-a", got)
	}
}

func TestParseProtocolRequiresConcreteCatalogSupportedToken(t *testing.T) {
	for _, raw := range []string{"", "auto", "unsupported"} {
		_, err := ParseProtocol(raw, ProviderOpenAI, func(_ Provider, protocol string) bool { return protocol != "unsupported" })
		if err == nil {
			t.Fatalf("ParseProtocol(%q) unexpectedly succeeded", raw)
		}
	}
}
