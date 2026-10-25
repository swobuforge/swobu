package routing

import (
	"reflect"
	"testing"
)

func TestSearchFallbackRouteImmutableAndValidated(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("research")
	workspace, err := RestoreWorkspaceWithSearchRoute(slug, nil, nil, nil, "cn_primary", []string{"cn_second", "cn_third"})
	if err != nil {
		t.Fatal(err)
	}
	input := workspace.SearchFallbackIDs()
	input[0] = "cn_unrelated"
	if !reflect.DeepEqual(workspace.SearchConnections(), []string{"cn_primary", "cn_second", "cn_third"}) {
		t.Fatalf("Search route leaked mutable slice: %#v", workspace.SearchConnections())
	}
	for _, tc := range []struct {
		name, primary string
		fallback      []string
	}{
		{"duplicate primary", "cn_primary", []string{"cn_primary"}},
		{"duplicate secondary", "cn_primary", []string{"cn_second", "cn_second"}},
		{"missing primary", "", []string{"cn_second"}},
		{"invalid ID", "cn_primary", []string{"bad/id"}},
		{"too many", "cn_primary", []string{"cn_1", "cn_2", "cn_3", "cn_4", "cn_5", "cn_6", "cn_7", "cn_8"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := RestoreWorkspaceWithSearchRoute(slug, nil, nil, nil, tc.primary, tc.fallback); err == nil {
				t.Fatal("invalid Search chain accepted")
			}
		})
	}
}

func TestSearchRouteConfigurationClearsFallbackAndPreservesOtherServices(t *testing.T) {
	slug, _ := ParseWorkspaceSlug("research")
	workspace, err := RestoreWorkspaceWithSearchRoute(slug, nil, nil, map[string]MCPMount{"docs": {ConnectionID: "cn_mcp", AllowedTools: []string{"search"}}}, "cn_primary", []string{"cn_fallback"})
	if err != nil {
		t.Fatal(err)
	}
	config, err := NewConfig([]Workspace{workspace})
	if err != nil {
		t.Fatal(err)
	}
	config, err = config.SetSearchRoute(slug, "cn_next", []string{"cn_other"})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := config.Workspace(slug)
	if !reflect.DeepEqual(after.SearchConnections(), []string{"cn_next", "cn_other"}) || after.MCPMounts()["docs"].ConnectionID != "cn_mcp" {
		t.Fatal("Search update modified independent MCP state")
	}
	config, err = config.SetSearchRoute(slug, "cn_solo", nil)
	if err != nil {
		t.Fatal(err)
	}
	after, _ = config.Workspace(slug)
	if !reflect.DeepEqual(after.SearchConnections(), []string{"cn_solo"}) {
		t.Fatal("single Search setter retained old fallbacks")
	}
	config, err = config.SetSearchRoute(slug, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	after, _ = config.Workspace(slug)
	if len(after.SearchConnections()) != 0 || !after.HasService(ServiceSearch) {
		t.Fatal("clear disabled Service or retained connection")
	}
}
