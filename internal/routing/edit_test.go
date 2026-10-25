package routing

import (
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"testing"
)

func TestRenameRoutePreservesTargetIdentity(t *testing.T) {
	config := testConfig(t, testTarget(t, "a"))
	slug, _ := ParseWorkspaceSlug("dev")
	from, _ := ParseRouteName("chat")
	to, _ := ParseRouteName("renamed")
	next, err := config.RenameRoute(slug, from, to)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := next.Workspace(slug)
	if _, exists := workspace.Route(to); !exists {
		t.Fatalf("renamed route %q not found", to.String())
	}
}

func TestTargetRequiresConnectionIdentity(t *testing.T) {
	target := testTarget(t, "a")
	if _, err := NewTarget(target.ID(), target.Model(), target.Protocol(), connectionidentity.ID{}); err == nil {
		t.Fatal("empty Connection ID admitted")
	}
}

func TestRoutingRejectsInvalidConnectionAttachments(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("research")
	if _, err := RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]MCPMount{
		"docs": {ConnectionID: "../docs", AllowedTools: []string{"read"}},
	}); err == nil {
		t.Fatal("MCP mount with invalid Connection ID admitted")
	}
	if _, err := RestoreWorkspaceWithSearchConnection(slug, nil, nil, nil, "search/fixture"); err == nil {
		t.Fatal("Search reference with invalid Connection ID admitted")
	}
	config, err := NewConfig([]Workspace{mustRestoreEmptyWorkspace(t, slug)})
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetServiceEnabled(slug, ServiceSearch, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = config.SetSearchRoute(slug, "UPPERCASE", nil); err == nil {
		t.Fatal("invalid Search Connection committed")
	}
}

func mustRestoreEmptyWorkspace(t *testing.T, slug WorkspaceSlug) Workspace {
	t.Helper()
	w, err := RestoreWorkspace(slug, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestIndependentWorkspaceAttachmentsSurviveCompositionEdits(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("dev")
	config := testConfig(t, testTarget(t, "original"))
	var err error
	config, err = config.SetServiceEnabled(slug, ServiceSearch, true)
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetServiceEnabled(slug, ServiceMCP, true)
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetSearchRoute(slug, "search", nil)
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetMCPMount(slug, "github", MCPMount{ConnectionID: "github", AllowedTools: []string{"read_file"}})
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := config.Workspace(slug)
	if workspace.SearchConnectionID() != "search" || workspace.MCPMounts()["github"].ConnectionID != "github" {
		t.Fatalf("composed Workspace lost an attachment: search=%q mcp=%#v", workspace.SearchConnectionID(), workspace.MCPMounts())
	}

	route, _ := ParseRouteName("alternate")
	config, err = config.CreateRoute(slug, route, testTarget(t, "additional"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ = config.Workspace(slug)
	if workspace.SearchConnectionID() != "search" || workspace.MCPMounts()["github"].ConnectionID != "github" {
		t.Fatalf("route creation lost independent attachments: search=%q mcp=%#v", workspace.SearchConnectionID(), workspace.MCPMounts())
	}

	config, err = config.DeleteMCPMount(slug, "github")
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ = config.Workspace(slug)
	if workspace.SearchConnectionID() != "search" || len(workspace.MCPMounts()) != 0 {
		t.Fatalf("MCP removal changed independent Search attachment: search=%q mcp=%#v", workspace.SearchConnectionID(), workspace.MCPMounts())
	}

	config, err = config.SetMCPMount(slug, "github", MCPMount{ConnectionID: "github", AllowedTools: []string{"read_file"}})
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetSearchRoute(slug, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ = config.Workspace(slug)
	if workspace.SearchConnectionID() != "" || workspace.MCPMounts()["github"].ConnectionID != "github" {
		t.Fatalf("Search removal changed independent MCP attachment: search=%q mcp=%#v", workspace.SearchConnectionID(), workspace.MCPMounts())
	}
}
