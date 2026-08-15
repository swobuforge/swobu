package openaifamily

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestModelCatalogPolicyComposesQueryProjectionAndMissingStatuses(t *testing.T) {
	policy := StandardBearerPolicy(profile.ProviderSpecBaseten).
		WithModelCatalogQuery(func(query url.Values) { query.Set("kind", "chat") }).
		WithModelCatalogProject(func(providerID profile.ProviderID, row modelcatalogopenai.ModelRow) (profile.ModelAuthoringOption, bool, error) {
			if row.ID() == "skip" {
				return profile.ModelAuthoringOption{}, false, nil
			}
			return profile.NewModelAuthoringOption(row.ID(), "display-"+row.ID(), string(providerID), "", "", nil, ""), true, nil
		}).
		WithModelCatalogMissingStatuses(http.StatusNotFound)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.URL.Query().Get("kind") != "chat" {
			t.Fatalf("catalog request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"z"},{"id":"skip"},{"id":"a"}]}`))
	}))
	defer server.Close()

	executor := NewExecutor(server.Client(), stubCredentialResolver{}, policy)
	models, err := executor.ListDeployments(context.Background(), provider.NewTargetSnapshot(
		"draft", "baseten", server.URL+"/v1", "env:BASETEN_API_KEY", protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE)))
	if err != nil {
		t.Fatal(err)
	}
	want := []profile.ModelAuthoringOption{
		profile.NewModelAuthoringOption("a", "display-a", "baseten", "", "", nil, ""),
		profile.NewModelAuthoringOption("z", "display-z", "baseten", "", "", nil, ""),
	}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %#v, want %#v", models, want)
	}
}

func TestModelCatalogPolicyDoesNotHideUnconfiguredFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	executor := NewExecutor(server.Client(), stubCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecBaseten).WithModelCatalogMissingStatuses(http.StatusNotFound))
	if _, err := executor.ListDeployments(context.Background(), provider.NewTargetSnapshot(
		"draft", "baseten", server.URL+"/v1", "env:BASETEN_API_KEY", protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE))); err == nil {
		t.Fatal("forbidden catalog response was hidden")
	}
}
