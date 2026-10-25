package connections

import (
	"reflect"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestReferencesEnumerateExactConsumers(t *testing.T) {
	base := fixtureConnection(t)
	provider, _ := routing.ParseProvider("openai", func(string) bool { return true })
	upstream, err := routing.NewStandardConnection(provider, "https://example.com", "env:ACCOUNT_ONE")
	if err != nil {
		t.Fatal(err)
	}
	other, err := routing.NewStandardConnection(provider, "https://example.com", "env:ACCOUNT_TWO")
	if err != nil {
		t.Fatal(err)
	}
	protocol, _ := routing.ParseProtocol("responses", provider, func(routing.Provider, string) bool { return true })
	model, _ := routing.ParseUpstreamModel("model")
	var workspaces []routing.Workspace
	for _, name := range []string{"two", "one"} {
		var routes []routing.Route
		for _, routeName := range []string{"second", "first"} {
			var targets []routing.Target
			for i, raw := range []string{base.ID().String(), "cn_other"} {
				value, _ := connectionidentity.Parse(raw)
				id, _ := routing.ParseTargetID(routeName + "-" + []string{"shared", "different"}[i])
				target, err := routing.NewTarget(id, model, protocol, value)
				if err != nil {
					t.Fatal(err)
				}
				targets = append(targets, target)
			}
			tier, err := routing.NewTier(targets)
			if err != nil {
				t.Fatal(err)
			}
			routeID, _ := routing.ParseRouteName(routeName)
			route, err := routing.NewRoute(routeID, []routing.Tier{tier})
			if err != nil {
				t.Fatal(err)
			}
			routes = append(routes, route)
		}
		slug, _ := routing.ParseWorkspaceSlug(name)
		workspace, err := routing.RestoreWorkspaceWithSearchConnection(slug, routes, nil, map[string]routing.MCPMount{"docs": {ConnectionID: base.ID().String()}, "other": {ConnectionID: "unmapped"}}, base.ID().String())
		if err != nil {
			t.Fatal(err)
		}
		workspaces = append(workspaces, workspace)
	}
	config, err := routing.NewConfig(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := New(base.ID(), "Shared account", spec, base.CreatedAt())
	if err != nil {
		t.Fatal(err)
	}
	want := []Consumer{
		{Workspace: "one", Kind: Model, Route: "first", Target: "first-shared"},
		{Workspace: "one", Kind: Model, Route: "second", Target: "second-shared"},
		{Workspace: "two", Kind: Model, Route: "first", Target: "first-shared"},
		{Workspace: "two", Kind: Model, Route: "second", Target: "second-shared"},
	}
	if got := References(config, connection); !reflect.DeepEqual(got, want) {
		t.Fatalf("model consumers: %+v", got)
	}
	rotatedSpec, err := NewModelSpec(other)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := connection.EditSameIdentity(connection.Revision(), connection.DisplayName(), rotatedSpec, connection.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got := References(config, rotated); !reflect.DeepEqual(got, want) {
		t.Fatalf("rotation changed stable consumers: %+v", got)
	}
	want = []Consumer{{Workspace: "one", Kind: MCP, Mount: "docs"}, {Workspace: "two", Kind: MCP, Mount: "docs"}}
	if got := References(config, base); !reflect.DeepEqual(got, want) {
		t.Fatalf("MCP consumers: %+v", got)
	}
	endpoint, _ := ParseEndpoint("https://example.com/search")
	search, _ := NewSearchSpec("search", endpoint, credentialref.Ref{})
	connection, err = New(base.ID(), "Search", search, base.CreatedAt())
	if err != nil {
		t.Fatal(err)
	}
	want = []Consumer{{Workspace: "one", Kind: Search}, {Workspace: "two", Kind: Search}}
	if got := References(config, connection); !reflect.DeepEqual(got, want) {
		t.Fatalf("Search consumers: %+v", got)
	}
	if got := References(routing.Config{}, connection); len(got) != 0 {
		t.Fatal("removed consumers retained")
	}
}
