package connections

import (
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestCurrentWorkspaceConnectionReferencesFailClosedWithLocation(t *testing.T) {
	slug, err := routing.ParseWorkspaceSlug("engineering")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mount  string
		search string
		want   error
		path   string
	}{
		{name: "unregistered imported MCP Connection", mount: "mcp-legacy", want: ErrNotFound, path: "workspaces.engineering.mcp_mounts.github.connection_id"},
		{name: "missing MCP Connection", mount: "cn_absent", want: ErrNotFound, path: "workspaces.engineering.mcp_mounts.github.connection_id"},
		{name: "unregistered imported Search Connection", search: "search-legacy", want: ErrNotFound, path: "workspaces.engineering.search.connection_id"},
		{name: "missing Search Connection", search: "cn_absent", want: ErrNotFound, path: "workspaces.engineering.search.connection_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mounts := map[string]routing.MCPMount{}
			if tc.mount != "" {
				mounts["github"] = routing.MCPMount{ConnectionID: tc.mount, AllowedTools: []string{"search_code"}}
			}
			ws, err := routing.RestoreWorkspaceWithSearchConnection(slug, nil, nil, mounts, tc.search)
			if err != nil {
				t.Fatal(err)
			}
			config, err := routing.NewConfig([]routing.Workspace{ws})
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateRoutingReferences(config, nil)
			if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("want %v at %s, got %v", tc.want, tc.path, err)
			}
		})
	}
}
