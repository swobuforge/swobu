package app_test

import (
	"context"
	"errors"
	"testing"

	operatormodelcatalog "github.com/metrofun/swobu/internal/app/operator/modelcatalog"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

func TestModelCatalogLoader_LoadsSelectedProviderCatalogPerEndpoint(t *testing.T) {
	endpoints := []testEndpointSpec{
		{name: "beta", selectedRef: "backend-b"},
		{name: "alpha", selectedRef: "backend-a"},
	}
	reader := fakeEndpointReader{endpoints: buildEndpoints(t, endpoints...)}
	providers := &fakeProviderExecutor{
		modelsByBackend: map[string][]string{
			"backend-a": {"gpt-4.1-mini", "gpt-4.1"},
			"backend-b": {"claude-sonnet"},
		},
	}

	snapshot, err := operatormodelcatalog.NewLoader(reader, providers).Load(context.Background())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(snapshot.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(snapshot.Entries))
	}
	if got := snapshot.Entries[0].EndpointName; got != "alpha" {
		t.Fatalf("first endpoint = %q, want %q", got, "alpha")
	}
	if got := snapshot.Entries[0].ProviderConfigRef; got != "backend-a" {
		t.Fatalf("first provider config ref = %q, want %q", got, "backend-a")
	}
	if got := snapshot.Entries[0].ModelIDs[0]; got != "gpt-4.1-mini" {
		t.Fatalf("first model = %q, want %q", got, "gpt-4.1-mini")
	}
	if got := providers.gotCatalogTargets[0].BackendRef; got != "backend-a" {
		t.Fatalf("catalog backend ref = %q, want %q", got, "backend-a")
	}
}

func TestModelCatalogLoader_KeepsEntryLocalErrors(t *testing.T) {
	reader := fakeEndpointReader{endpoints: buildEndpoints(t, testEndpointSpec{name: "alpha", selectedRef: "backend-a"})}
	providers := &fakeProviderExecutor{
		modelCatalogErr: errors.New("backend down"),
	}

	snapshot, err := operatormodelcatalog.NewLoader(reader, providers).Load(context.Background())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(snapshot.Entries))
	}
	if got := snapshot.Entries[0].Error; got != "backend down" {
		t.Fatalf("entry error = %q, want %q", got, "backend down")
	}
}

type testEndpointSpec struct {
	name        string
	selectedRef string
}

func buildEndpoints(t *testing.T, specs ...testEndpointSpec) []endpointintent.Endpoint {
	t.Helper()

	endpoints := make([]endpointintent.Endpoint, 0, len(specs))
	for _, spec := range specs {
		name, err := endpointintent.ParseEndpointName(spec.name)
		if err != nil {
			t.Fatalf("ParseEndpointName returned error: %v", err)
		}
		ref, err := endpointintent.ParseProviderConfigRef(spec.selectedRef)
		if err != nil {
			t.Fatalf("ParseProviderConfigRef returned error: %v", err)
		}
		providerSpec, err := endpointintent.ParseProviderSpec("custom")
		if err != nil {
			t.Fatalf("ParseProviderSpec returned error: %v", err)
		}
		providerConfig, err := endpointintent.NewProviderConfig(ref, providerSpec, "https://example.test/v1", "", protocolsurface.ChatCompletions)
		if err != nil {
			t.Fatalf("NewProviderConfig returned error: %v", err)
		}
		endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{providerConfig}, ref)
		if err != nil {
			t.Fatalf("NewEndpoint returned error: %v", err)
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints
}
