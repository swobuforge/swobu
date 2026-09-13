package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestShareModelsPreserveWorkspaceCatalogAndRouteDefaultOnly(t *testing.T) {
	workspace := shareModelsWorkspace(t)

	workspaceModels, err := (workspaceBoundIngress{workspace: workspace, workspaceScope: true}).ListModels(context.Background(), exchange.ListModelsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got := modelIDs(workspaceModels); workspaceModels.DefaultModelID != "coding" || !slices.Equal(got, []string{"coding", "writing"}) {
		t.Fatalf("workspace Share default/models = %q/%v", workspaceModels.DefaultModelID, got)
	}

	writingName, _ := routing.ParseRouteName("writing")
	writing, _ := workspace.Route(writingName)
	projected, err := routing.NewWorkspace(workspace.Slug(), writingName, []routing.Route{writing})
	if err != nil {
		t.Fatal(err)
	}
	routeModels, err := (workspaceBoundIngress{workspace: projected}).ListModels(context.Background(), exchange.ListModelsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got := modelIDs(routeModels); routeModels.DefaultModelID != routing.PublicDefaultRouteID || len(got) != 0 {
		t.Fatalf("route Share default/models = %q/%v", routeModels.DefaultModelID, got)
	}
}

func TestRouteShareHTTPDoesNotReachSiblingRoute(t *testing.T) {
	workspace := shareModelsWorkspace(t)
	codingName, _ := routing.ParseRouteName("coding")
	coding, _ := workspace.Route(codingName)
	projected, err := routing.NewWorkspace(workspace.Slug(), codingName, []routing.Route{coding})
	if err != nil {
		t.Fatal(err)
	}
	var targets []string
	runtime := terminalProjectionRuntime{
		RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(),
		transport: terminalProjectionTransport{documentBody: `{"id":"resp","model":"coding-model","output":[]}`, onSend: func(target provider.TargetSnapshot) {
			targets = append(targets, target.TargetID)
		}},
	}
	ingress := exchange.NewIngress(nil, runtime, exchange.RuntimePoliciesSpec{})
	handler := NewHandler(workspaceBoundIngress{workspace: projected, ingress: ingress}, nil)
	request := httptest.NewRequest(http.MethodPost, "/c/dev/responses", strings.NewReader(`{"model":"writing","input":"hello"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-Id", "req_route_share_isolation")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !slices.Equal(targets, []string{"coding-target"}) {
		t.Fatalf("Route Share attempted targets %v, want only coding-target", targets)
	}
}

func shareModelsWorkspace(t *testing.T) routing.Workspace {
	t.Helper()
	slug, _ := routing.ParseWorkspaceSlug("dev")
	routes := make([]routing.Route, 0, 2)
	for _, raw := range []string{"coding", "writing"} {
		name, _ := routing.ParseRouteName(raw)
		targetID, _ := routing.ParseTargetID(raw + "-target")
		model, _ := routing.ParseUpstreamModel(raw + "-model")
		provider, _ := routing.ParseProvider("custom", func(candidate string) bool { return candidate == "custom" })
		connection, _ := routing.NewCustomConnection(provider, "https://example.test/v1", nil)
		protocol, _ := routing.ParseProtocol("responses", provider, func(routing.Provider, string) bool { return true })
		target, err := routing.NewTarget(targetID, model, protocol, connection)
		if err != nil {
			t.Fatal(err)
		}
		tier, err := routing.NewTier([]routing.Target{target})
		if err != nil {
			t.Fatal(err)
		}
		route, err := routing.NewRoute(name, []routing.Tier{tier})
		if err != nil {
			t.Fatal(err)
		}
		routes = append(routes, route)
	}
	defaultRoute, _ := routing.ParseRouteName("coding")
	workspace, err := routing.NewWorkspace(slug, defaultRoute, routes)
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func modelIDs(out exchange.ListModelsOutput) []string {
	ids := make([]string, 0, len(out.Models))
	for _, model := range out.Models {
		ids = append(ids, model.ID)
	}
	return ids
}
