// Integration proof for the daemon-owned operator control plane.
// Proves: HTTP client → EndpointControlHandler → OperatorEndpointStore → persistence → response.
package endpointintent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metrofun/swobu/internal/adapters/inbound/httpapi"
	operatorclient "github.com/metrofun/swobu/internal/app/operator/client"
	operatorendpoints "github.com/metrofun/swobu/internal/app/operator/endpoints"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

// fakeRepo is an in-memory EndpointIntentRepository for integration testing.
type fakeRepo struct {
	data map[string]endpointintent.Endpoint
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{data: make(map[string]endpointintent.Endpoint)}
}

func (r *fakeRepo) GetEndpoint(_ context.Context, name endpointintent.EndpointName) (endpointintent.Endpoint, error) {
	ep, ok := r.data[name.String()]
	if !ok {
		return endpointintent.Endpoint{}, operatorendpoints.CommandError{
			Code:    operatorendpoints.CommandNotFound,
			Message: "endpoint not found: " + name.String(),
		}
	}
	return ep, nil
}

func (r *fakeRepo) ListEndpoints(_ context.Context) ([]endpointintent.Endpoint, error) {
	result := make([]endpointintent.Endpoint, 0, len(r.data))
	for _, ep := range r.data {
		result = append(result, ep)
	}
	return result, nil
}

func (r *fakeRepo) SaveEndpoints(_ context.Context, endpoints []endpointintent.Endpoint) error {
	r.data = make(map[string]endpointintent.Endpoint)
	for _, ep := range endpoints {
		r.data[ep.Name().String()] = ep
	}
	return nil
}

func TestDaemonOperatorControlPlane_ListGetPutDelete(t *testing.T) {
	repo := newFakeRepo()
	store := operatorendpoints.NewOperatorEndpointStore(repo)

	mux := http.NewServeMux()
	handler := httpapi.NewEndpointControlHandler(
		func(ctx context.Context) ([]endpointintent.Endpoint, error) { return store.List(ctx) },
		func(ctx context.Context, name string) (endpointintent.Endpoint, error) { return store.Get(ctx, name) },
		func(ctx context.Context, ep endpointintent.Endpoint) (endpointintent.Endpoint, error) {
			return store.Put(ctx, ep)
		},
		func(ctx context.Context, name string) error { return store.Delete(ctx, name) },
	)
	mux.Handle("/_swobu/endpoints", handler)
	mux.Handle("/_swobu/endpoints/", handler)

	server := httptest.NewServer(mux)
	defer server.Close()

	client := operatorclient.New(server.Client(), server.URL)
	ctx := context.Background()

	// 1. List starts empty.
	list, err := client.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List: got %d endpoints, want 0", len(list))
	}

	// 2. Put a new endpoint.
	name := mustEndpointName(t, "test-workspace")
	ref := mustProviderConfigRef(t, compatibility.PrimaryTargetSelector)
	spec := mustProviderSpec(t, "openai")
	pc := mustProviderConfig(t, ref, spec, "https://api.openai.com/v1", "sk-test", "gpt-4", protocolsurface.ChatCompletions)
	ep := mustNewEndpoint(t, name, []endpointintent.ProviderConfig{pc}, ref)

	saved, err := client.Put(ctx, ep)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if saved.Name().String() != "test-workspace" {
		t.Fatalf("Put: name = %q, want %q", saved.Name().String(), "test-workspace")
	}

	// 3. Get the endpoint.
	got, err := client.Get(ctx, "test-workspace")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name().String() != "test-workspace" {
		t.Fatalf("Get: name = %q, want %q", got.Name().String(), "test-workspace")
	}
	if got.SelectedProviderConfigRef().String() != compatibility.PrimaryTargetSelector {
		t.Fatalf("Get: selected ref = %q, want %q", got.SelectedProviderConfigRef().String(), compatibility.PrimaryTargetSelector)
	}

	// 4. List now returns one endpoint.
	list, err = client.List(ctx)
	if err != nil {
		t.Fatalf("List after Put: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List after Put: got %d endpoints, want 1", len(list))
	}

	// 5. Delete the endpoint.
	if err := client.Delete(ctx, "test-workspace"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// 6. List is empty again.
	list, err = client.List(ctx)
	if err != nil {
		t.Fatalf("List after Delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List after Delete: got %d endpoints, want 0", len(list))
	}

	// 7. Get after delete returns not found.
	_, err = client.Get(ctx, "test-workspace")
	if err == nil {
		t.Fatal("Get after delete: want error, got nil")
	}
}

func mustEndpointName(t *testing.T, s string) endpointintent.EndpointName {
	t.Helper()
	name, err := endpointintent.ParseEndpointName(s)
	if err != nil {
		t.Fatalf("parse endpoint name %q: %v", s, err)
	}
	return name
}

func mustProviderConfigRef(t *testing.T, s string) endpointintent.ProviderConfigRef {
	t.Helper()
	ref, err := endpointintent.ParseProviderConfigRef(s)
	if err != nil {
		t.Fatalf("parse provider config ref %q: %v", s, err)
	}
	return ref
}

func mustProviderSpec(t *testing.T, s string) endpointintent.ProviderSpec {
	t.Helper()
	spec, err := endpointintent.ParseProviderSpec(s)
	if err != nil {
		t.Fatalf("parse provider spec %q: %v", s, err)
	}
	return spec
}

func mustProviderConfig(t *testing.T, ref endpointintent.ProviderConfigRef, spec endpointintent.ProviderSpec, baseURL, credRef, modelID string, kind protocolsurface.Kind) endpointintent.ProviderConfig {
	t.Helper()
	pc, err := endpointintent.NewProviderConfig(ref, spec, baseURL, credRef, kind)
	if err != nil {
		t.Fatalf("new provider config: %v", err)
	}
	pc, err = pc.WithModelID(modelID)
	if err != nil {
		t.Fatalf("with model id: %v", err)
	}
	return pc
}

func mustNewEndpoint(t *testing.T, name endpointintent.EndpointName, configs []endpointintent.ProviderConfig, selectedRef endpointintent.ProviderConfigRef) endpointintent.Endpoint {
	t.Helper()
	ep, err := endpointintent.NewEndpoint(name, configs, selectedRef)
	if err != nil {
		t.Fatalf("new endpoint: %v", err)
	}
	return ep
}
