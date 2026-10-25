package routing

import (
	"errors"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"strings"
	"testing"
)

func TestWorkspaceMCPMountAliasesFollowPublishedIdentityGrammar(t *testing.T) {
	slug, err := ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"1docs", "docs\t", "docs:extra", "é", strings.Repeat("a", 33), "swobu"} {
		t.Run(alias, func(t *testing.T) {
			_, err := RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]MCPMount{alias: {ConnectionID: "docs", AllowedTools: []string{"search"}}})
			if err == nil {
				t.Fatal("Workspace accepted an unpublishable MCP alias")
			}
		})
	}
	for _, alias := range []string{"a", "docs-2", strings.Repeat("a", 32)} {
		workspace, err := RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]MCPMount{alias: {ConnectionID: "docs", AllowedTools: []string{"search"}}})
		if err != nil || workspace.MCPMounts()[alias].ConnectionID != "docs" {
			t.Fatalf("valid alias %q changed: %v", alias, err)
		}
	}
}

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
	provider, _ := ParseProvider("openai", func(string) bool { return true })
	connection, _ := connectionidentity.Parse("cn_shared")
	target, err := NewTarget(targetID, model, testProtocol(t, provider, "responses"), connection)
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
	workspace, err := NewWorkspace(slug, []Route{route})
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
		workspace, _ := NewWorkspace(slug, []Route{route})
		workspaces = append(workspaces, workspace)
	}
	if _, err := NewConfig(workspaces); err != nil {
		t.Fatalf("NewConfig rejected workspace-scoped target IDs: %v", err)
	}
}

func TestRestoreWorkspaceRejectsActiveGenerationLedgerMismatch(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("dev")
	name, _ := ParseRouteName("chat")
	target := testTarget(t, "same")
	tier, _ := NewTier([]Target{target})
	route, _ := NewRoute(name, []Tier{tier})
	for _, version := range []TargetVersion{2, 3} {
		if _, err := RestoreWorkspace(slug, []Route{route}, map[TargetID]TargetVersion{target.id: version}); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("ledger version %d error = %v, want mismatch rejection", version, err)
		}
	}
}

func TestTargetGenerationHistorySurvivesRouteDeletionAndDisplayEdit(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("dev")
	firstName, _ := ParseRouteName("first")
	secondName, _ := ParseRouteName("second")
	x := testTarget(t, "x")
	keep := testTarget(t, "keep")
	firstTier, _ := NewTier([]Target{x})
	secondTier, _ := NewTier([]Target{keep})
	first, _ := NewRoute(firstName, []Tier{firstTier})
	second, _ := NewRoute(secondName, []Tier{secondTier})
	workspace, _ := NewWorkspace(slug, []Route{first, second})
	config, _ := NewConfig([]Workspace{workspace})

	withoutRoute, err := config.DeleteRoute(slug, firstName)
	if err != nil {
		t.Fatal(err)
	}
	updatedConfig, err := withoutRoute.SetWorkspaceDisplayName(slug, "Research")
	if err != nil {
		t.Fatal(err)
	}
	recreated, err := updatedConfig.CreateRoute(slug, firstName, x)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ = recreated.Workspace(slug)
	route, _ := workspace.Route(firstName)
	if got := route.Tiers()[0].Targets()[0].Version(); got != 2 {
		t.Fatalf("recreated route target generation = %d, want 2", got)
	}
}

func TestDeletingWorkspaceDeletesItsTargetIdentityNamespace(t *testing.T) {
	config := testConfig(t, testTarget(t, "x"))
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	deleted, err := config.DeleteWorkspace(slug)
	if err != nil {
		t.Fatal(err)
	}
	recreated, err := deleted.CreateWorkspace(WorkspaceSeed{Slug: slug, Route: routeName, Target: testTarget(t, "x")})
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := recreated.Workspace(slug)
	route, _ := workspace.Route(routeName)
	if got := route.Tiers()[0].Targets()[0].Version(); got != initialTargetVersion {
		t.Fatalf("new workspace target generation = %d, want %d", got, initialTargetVersion)
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
	if _, err := NewWorkspace(slug, []Route{first, second}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewWorkspace error = %v, want duplicate target rejection", err)
	}
}

func TestModelPatternsAndTargetIDsHaveIndependentGrammars(t *testing.T) {
	validShared := []string{"a", "Model.Name-1_v2", strings.Repeat("x", 128)}
	invalidShared := []string{"", ".", "..", "a b", " padded", strings.Repeat("x", 129)}
	for _, raw := range validShared {
		if _, err := ParseRouteName(raw); err != nil {
			t.Errorf("ParseRouteName(%q): %v", raw, err)
		}
		if _, err := ParseTargetID(raw); err != nil {
			t.Errorf("ParseTargetID(%q): %v", raw, err)
		}
	}
	for _, raw := range invalidShared {
		if _, err := ParseRouteName(raw); err == nil {
			t.Errorf("ParseRouteName(%q) unexpectedly succeeded", raw)
		}
		if _, err := ParseTargetID(raw); err == nil {
			t.Errorf("ParseTargetID(%q) unexpectedly succeeded", raw)
		}
	}
	for _, raw := range []string{"foo/bar", "a?b", "claude-*", "gpt-[45]*"} {
		if _, err := ParseRouteName(raw); err != nil {
			t.Errorf("valid model glob %q: %v", raw, err)
		}
		if _, err := ParseTargetID(raw); err == nil {
			t.Errorf("Target ID cannot be glob %q", raw)
		}
	}
	if _, err := ParseRouteName("swobu"); err != nil {
		t.Errorf("valid model name: %v", err)
	}
	if _, err := ParseRouteName("default"); err != nil {
		t.Errorf("default is an ordinary exact route pattern: %v", err)
	}
}

func TestWorkspaceResolveRouteRequiresExactPattern(t *testing.T) {
	chatName, _ := ParseRouteName("chat")
	exactName, _ := ParseRouteName("exact")
	chatTier, _ := NewTier([]Target{testTarget(t, "chat-target")})
	exactTier, _ := NewTier([]Target{testTarget(t, "exact-target")})
	chatRoute, _ := NewRoute(chatName, []Tier{chatTier})
	exactRoute, _ := NewRoute(exactName, []Tier{exactTier})
	slug, _ := ParseWorkspaceSlug("dev")
	workspace, err := NewWorkspace(slug, []Route{chatRoute, exactRoute})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		requested string
		want      string
	}{
		{requested: "exact", want: "exact"},
		{requested: "chat", want: "chat"},
	} {
		route, err := workspace.ResolveRoute(test.requested)
		if err != nil || route.Name().String() != test.want {
			t.Fatalf("ResolveRoute(%q) = %q, %v, want %q", test.requested, route.Name().String(), err, test.want)
		}
	}
	for _, requested := range []string{"fixed-client-model", "provider/model:not-a-route", "default"} {
		if _, err := workspace.ResolveRoute(requested); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ResolveRoute(%q) error = %v, want ErrNotFound", requested, err)
		}
	}
	for _, requested := range []string{"", " \t"} {
		if _, err := workspace.ResolveRoute(requested); !errors.Is(err, ErrEmptyRequestedRoute) {
			t.Fatalf("ResolveRoute(%q) error = %v, want ErrEmptyRequestedRoute", requested, err)
		}
	}
}

func TestEmptyWorkspaceIsValidButHasNoResolvableRoute(t *testing.T) {
	slug, err := ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	config, err := NewConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.CreateEmptyWorkspace(EmptyWorkspaceSeed{Slug: slug})
	if err != nil {
		t.Fatal(err)
	}
	workspace, ok := config.Workspace(slug)
	if !ok || len(workspace.Routes()) != 0 {
		t.Fatalf("routes=%d", len(workspace.Routes()))
	}
	if _, err := workspace.ResolveRoute("default"); !errors.Is(err, ErrServiceDisabled) {
		t.Fatalf("ResolveRoute error=%v, want ErrNoRoute", err)
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
	replacement := testTarget(t, "replacement")
	workspace, _ := clone.Workspace(slug)
	route, _ := workspace.Route(routeName)
	spec := route.Spec()
	spec.Tiers[0].Targets[0].Settings = TargetSettings{Model: replacement.Model(), Protocol: replacement.Protocol(), ConnectionID: replacement.ConnectionID()}
	if _, err := clone.ApplyRouteSpec(slug, routeName, spec); err != nil {
		t.Fatal(err)
	}

	workspace, _ = original.Workspace(slug)
	route, _ = workspace.Route(routeName)
	if got := route.Tiers()[0].Targets()[0].Model().String(); got != "upstream-a" {
		t.Fatalf("original model = %q after editing clone, want upstream-a", got)
	}
}

func TestParseProtocolRequiresConcreteCatalogSupportedToken(t *testing.T) {
	for _, raw := range []string{"", "not_a_protocol"} {
		_, err := ParseProtocol(raw, Provider("openai"), func(_ Provider, protocol string) bool { return protocol == "responses" })
		if err == nil {
			t.Fatalf("ParseProtocol(%q) unexpectedly succeeded", raw)
		}
	}
}
