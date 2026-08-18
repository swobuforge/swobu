package exchange

import (
	"context"
	"slices"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

type modelDiscoveryWorkspaceLookup struct{ workspace routing.Workspace }

func (l modelDiscoveryWorkspaceLookup) GetWorkspace(context.Context, routing.WorkspaceSlug) (routing.Workspace, error) {
	return l.workspace, nil
}

func TestListModelsReturnsRouteIDsLexicographically(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("dev")
	var routes []routing.Route
	for index, raw := range []string{"claude-fast", "chat", "alpha"} {
		name, _ := routing.ParseRouteName(raw)
		tier, _ := routing.NewTier([]routing.Target{requestpathTarget(t, "catalog-target-"+string(rune('a'+index)))})
		route, _ := routing.NewRoute(name, []routing.Tier{tier})
		routes = append(routes, route)
	}
	defaultRoute, _ := routing.ParseRouteName("chat")
	workspace, err := routing.NewWorkspace(slug, defaultRoute, routes)
	if err != nil {
		t.Fatal(err)
	}
	ingress := NewIngress(modelDiscoveryWorkspaceLookup{workspace: workspace}, nil, RuntimePoliciesSpec{})

	want := []string{"alpha", "chat", "claude-fast"}
	for attempt := 0; attempt < 20; attempt++ {
		out, err := ingress.ListModels(context.Background(), ListModelsInput{Workspace: slug})
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0, len(out.Models))
		for _, model := range out.Models {
			got = append(got, model.ID)
		}
		if out.DefaultModelID != "chat" || !slices.Equal(got, want) {
			t.Fatalf("attempt %d: default/models = %q/%v, want chat/%v", attempt, out.DefaultModelID, got, want)
		}
	}
}
