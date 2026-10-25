package routing

import (
	"errors"
	"testing"
)

func TestExplicitServiceLifecyclePreservesEnabledEmptyState(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("research")
	config, err := NewConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.CreateEmptyWorkspace(EmptyWorkspaceSeed{Slug: slug})
	if err != nil {
		t.Fatal(err)
	}
	initial, _ := config.Workspace(slug)
	if len(initial.EnabledServices()) != 0 {
		t.Fatalf("empty Workspace has Services: %v", initial.EnabledServices())
	}
	if _, err := config.SetSearchRoute(slug, "cn_search", nil); !errors.Is(err, ErrServiceDisabled) {
		t.Fatalf("disabled Search accepted config: %v", err)
	}
	if _, err := config.SetMCPMount(slug, "github", MCPMount{ConnectionID: "cn_mcp"}); !errors.Is(err, ErrServiceDisabled) {
		t.Fatalf("disabled MCP accepted config: %v", err)
	}
	config, err = config.SetServiceEnabled(slug, ServiceSearch, true)
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetServiceEnabled(slug, ServiceMCP, true)
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetServiceEnabled(slug, ServiceModels, true)
	if err != nil {
		t.Fatal(err)
	}
	enabled, _ := config.Workspace(slug)
	if got := enabled.EnabledServices(); len(got) != 3 || got[0] != ServiceModels || got[1] != ServiceMCP || got[2] != ServiceSearch {
		t.Fatalf("enabled-empty lifecycle: %v", got)
	}
	// Snapshot collection is defensive, so callers cannot mutate published state.
	services := enabled.EnabledServices()
	services[0] = ServiceKind("images")
	updated, err := config.SetServiceEnabled(slug, ServiceMCP, false)
	if err != nil {
		t.Fatal(err)
	}
	empty, _ := updated.Workspace(slug)
	if empty.HasService(ServiceMCP) || !empty.HasService(ServiceModels) || !empty.HasService(ServiceSearch) {
		t.Fatal("independent Services changed when disabling empty MCP")
	}
}

func TestDisableNonEmptyServiceRejectedWithoutClearingChildren(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("research")
	config, _ := NewConfig(nil)
	config, err := config.CreateEmptyWorkspace(EmptyWorkspaceSeed{Slug: slug})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []ServiceKind{ServiceMCP, ServiceSearch} {
		config, err = config.SetServiceEnabled(slug, kind, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	config, err = config.SetMCPMount(slug, "github", MCPMount{ConnectionID: "cn_github", AllowedTools: []string{"read_issue"}})
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetSearchRoute(slug, "cn_search", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []ServiceKind{ServiceMCP, ServiceSearch} {
		if _, err := config.SetServiceEnabled(slug, kind, false); !errors.Is(err, ErrServiceNotEmpty) {
			t.Fatalf("%s disable: %v", kind, err)
		}
	}
	preserved, _ := config.Workspace(slug)
	if preserved.MCPMounts()["github"].ConnectionID != "cn_github" || preserved.SearchConnectionID() != "cn_search" {
		t.Fatal("disable changed configuration")
	}
	config, err = config.DeleteMCPMount(slug, "github")
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetSearchRoute(slug, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []ServiceKind{ServiceMCP, ServiceSearch} {
		config, err = config.SetServiceEnabled(slug, kind, false)
		if err != nil {
			t.Fatalf("empty %s must disable: %v", kind, err)
		}
	}
}

func TestServiceKindRejectsUnreleasedNamesAndDuplicateDeclarations(t *testing.T) {
	for _, value := range []string{"decisions", "images", "Models", "", "mcp ", "model"} {
		if _, err := ParseServiceKind(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	slug, _ := ParseWorkspaceSlug("research")
	w, err := RestoreWorkspace(slug, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.WithServices([]ServiceKind{ServiceModels, ServiceModels}); err == nil {
		t.Fatal("duplicate services accepted")
	}
	if _, err = w.WithServices([]ServiceKind{"images"}); err == nil {
		t.Fatal("unsupported service accepted")
	}
}

func TestEnabledEmptyModelsRouteLifecycleHasNoArtificialLastRouteBlocker(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("research")
	config, _ := NewConfig(nil)
	config, err := config.CreateEmptyWorkspace(EmptyWorkspaceSeed{Slug: slug})
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetServiceEnabled(slug, ServiceModels, true)
	if err != nil {
		t.Fatal(err)
	}
	name, _ := ParseRouteName("coding")
	config, err = config.CreateRoute(slug, name, testTarget(t, "first"))
	if err != nil {
		t.Fatalf("enabled-empty Models cannot create first route: %v", err)
	}
	w, _ := config.Workspace(slug)
	if len(w.Routes()) != 1 || w.Routes()[0].Name() != name {
		t.Fatal("first route not configured")
	}
	if _, err := config.SetServiceEnabled(slug, ServiceModels, false); !errors.Is(err, ErrServiceNotEmpty) {
		t.Fatalf("disable configured Models: %v", err)
	}
	config, err = config.DeleteRoute(slug, name)
	if err != nil {
		t.Fatalf("last route cannot be removed: %v", err)
	}
	w, _ = config.Workspace(slug)
	if !w.HasService(ServiceModels) || len(w.Routes()) != 0 {
		t.Fatal("Models did not become enabled-empty")
	}
	config, err = config.SetServiceEnabled(slug, ServiceModels, false)
	if err != nil {
		t.Fatal(err)
	}
	w, _ = config.Workspace(slug)
	if w.HasService(ServiceModels) {
		t.Fatal("Models still enabled")
	}
}
