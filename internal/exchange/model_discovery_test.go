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

func TestListModelsFollowsModelRoutePriority(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("dev")
	var routes []routing.Route
	for index, raw := range []string{"claude-fast", "chat", "alpha"} {
		name, _ := routing.ParseRouteName(raw)
		tier, _ := routing.NewTier([]routing.Target{requestpathTarget(t, "catalog-target-"+string(rune('a'+index)))})
		route, _ := routing.NewRoute(name, []routing.Tier{tier})
		routes = append(routes, route)
	}
	workspace, err := routing.NewWorkspace(slug, routes)
	if err != nil {
		t.Fatal(err)
	}
	ingress := NewIngress(modelDiscoveryWorkspaceLookup{workspace: workspace}, nil, RuntimePoliciesSpec{})

	want := []string{"claude-fast", "chat", "alpha"}
	for attempt := 0; attempt < 20; attempt++ {
		out, err := ingress.ListModels(context.Background(), ListModelsInput{Workspace: slug})
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0, len(out.Models))
		for _, model := range out.Models {
			got = append(got, model.ID)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("attempt %d: models = %v, want %v", attempt, got, want)
		}
	}
}

func TestGlobRoutesAreNotAdvertisedAsClientModelIDs(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("dev")
	var routes []routing.Route
	for index, pattern := range []string{"claude-*", "chat", "deepseek/*", "*"} {
		name, _ := routing.ParseRouteName(pattern)
		target := requestpathTarget(t, "catalog-glob-"+string(rune('a'+index)))
		tier, _ := routing.NewTier([]routing.Target{target})
		route, _ := routing.NewRoute(name, []routing.Tier{tier})
		routes = append(routes, route)
	}
	workspace, err := routing.NewWorkspace(slug, routes)
	if err != nil {
		t.Fatal(err)
	}
	models := ListModelsWithWorkspace(workspace)
	if len(models.Models) != 1 || models.Models[0].ID != "chat" {
		t.Fatalf("wildcard advertised as concrete model: %+v", models)
	}
}
