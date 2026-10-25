package routing

import (
	"errors"
	"testing"
)

func TestWorkspaceEditsKeepStoreIssuedIdentity(t *testing.T) {
	slug, err := ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := RestoreWorkspace(slug, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	workspace = workspace.WithManagementIncarnation("incarnation-1").WithManagementRevision(42).WithConfigurationRevision("publication-7")
	config, err := NewConfig([]Workspace{workspace})
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetServiceEnabled(slug, ServiceSearch, true)
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetSearchRoute(slug, "cn_search", nil)
	if err != nil {
		t.Fatal(err)
	}
	actual, exists := config.Workspace(slug)
	if !exists {
		t.Fatal("Workspace disappeared")
	}
	if actual.ManagementIncarnation() != "incarnation-1" || actual.ManagementRevision() != 42 || actual.ConfigurationRevision() != "publication-7" {
		t.Fatalf("semantic edit erased store-issued identity: incarnation=%q revision=%d publication=%q", actual.ManagementIncarnation(), actual.ManagementRevision(), actual.ConfigurationRevision())
	}
}

func TestWorkspaceListingHasCanonicalOrder(t *testing.T) {
	workspaces := make([]Workspace, 0, 3)
	for _, raw := range []string{"zebra", "alpha", "middle"} {
		slug, err := ParseWorkspaceSlug(raw)
		if err != nil {
			t.Fatal(err)
		}
		workspace, err := RestoreWorkspace(slug, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		workspaces = append(workspaces, workspace)
	}
	config, err := NewConfig(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		got := config.Workspaces()
		for index, want := range []string{"alpha", "middle", "zebra"} {
			if got[index].Slug().String() != want {
				t.Fatalf("iteration %d index %d = %q, want %q", i, index, got[index].Slug(), want)
			}
		}
	}
}

func TestWorkspaceRejectsUnconfiguredRouteInsteadOfUsingDefault(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("research")
	name, _ := ParseRouteName("coding")
	tier, err := NewTier([]Target{testTarget(t, "target")})
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
	for _, requested := range []string{"typo", "unconfigured-model", "provider/model", " coding ", "../coding"} {
		if _, err := workspace.ResolveRoute(requested); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ResolveRoute(%q) = %v, want ErrNotFound", requested, err)
		}
	}
}

func TestInvalidMCPMountEditNeverPublishes(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("research")
	config, _ := NewConfig(nil)
	config, _ = config.CreateEmptyWorkspace(EmptyWorkspaceSeed{Slug: slug})
	config, _ = config.SetServiceEnabled(slug, ServiceMCP, true)

	for _, attempt := range []struct {
		alias string
		mount MCPMount
	}{
		{alias: "Invalid", mount: MCPMount{ConnectionID: "cn_valid"}},
		{alias: "mount", mount: MCPMount{ConnectionID: "CN_INVALID"}},
		{alias: "mount", mount: MCPMount{ConnectionID: "cn_valid", AllowedTools: []string{"*"}}},
		{alias: "mount", mount: MCPMount{ConnectionID: "cn_valid", AllowedTools: []string{"read\nwrite"}}},
	} {
		if _, err := config.SetMCPMount(slug, attempt.alias, attempt.mount); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("SetMCPMount(%q, %#v) = %v, want invalid config", attempt.alias, attempt.mount, err)
		}
		workspace, _ := config.Workspace(slug)
		if len(workspace.MCPMounts()) != 0 {
			t.Fatal("failed edit changed the original Workspace")
		}
	}
}
