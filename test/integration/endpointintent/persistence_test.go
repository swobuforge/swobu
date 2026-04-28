package endpointintent_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/metrofun/swobu/internal/adapters/outbound/persistence"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

func TestEndpointIntentStore_RoundTripsProviderConfigOrderAndSelectedConfig(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	endpoints := []endpointintent.Endpoint{
		mustEndpoint(
			t,
			"zeta",
			"config-a",
			providerConfigSpec{ref: "config-b", providerSpec: "custom", baseURL: "https://b.test/v1", protocol: protocolsurface.Responses},
			providerConfigSpec{ref: "config-a", providerSpec: "custom", baseURL: "https://a.test/v1", protocol: protocolsurface.ChatCompletions},
		),
		mustEndpoint(
			t,
			"alpha",
			"config-c",
			providerConfigSpec{ref: "config-c", providerSpec: "openai", protocol: protocolsurface.ChatCompletions},
		),
	}

	if err := store.SaveEndpoints(context.Background(), endpoints); err != nil {
		t.Fatalf("SaveEndpoints returned error: %v", err)
	}

	got, err := store.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("ListEndpoints returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(endpoints) = %d, want 2", len(got))
	}
	if got[0].Name().String() != "alpha" || got[1].Name().String() != "zeta" {
		t.Fatalf("endpoint order = [%q %q], want [alpha zeta]", got[0].Name().String(), got[1].Name().String())
	}

	zeta := got[1]
	providerConfigs := zeta.ProviderConfigs()
	if len(providerConfigs) != 2 {
		t.Fatalf("len(providerConfigs) = %d, want 2", len(providerConfigs))
	}
	if providerConfigs[0].Ref().String() != "config-b" || providerConfigs[1].Ref().String() != "config-a" {
		t.Fatalf("provider config order = [%q %q], want [config-b config-a]", providerConfigs[0].Ref().String(), providerConfigs[1].Ref().String())
	}
	if got := zeta.SelectedProviderConfig().Ref().String(); got != "config-a" {
		t.Fatalf("selected provider config = %q, want config-a", got)
	}
}

func TestEndpointIntentStore_GetEndpointMissingDoesNotAutoCreate(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}

	_, err = store.GetEndpoint(context.Background(), name)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error = %v, want fs.ErrNotExist", err)
	}
}

type providerConfigSpec struct {
	ref          string
	providerSpec string
	baseURL      string
	credential   string
	protocol     protocolsurface.Kind
}

func newStore(t *testing.T) persistence.EndpointIntentStore {
	t.Helper()

	store, err := persistence.NewEndpointIntentStore(persistence.EndpointIntentStoreConfig{
		Path: filepath.Join(t.TempDir(), "endpoints.json"),
	})
	if err != nil {
		t.Fatalf("NewEndpointIntentStore returned error: %v", err)
	}
	return store
}

func mustEndpoint(t *testing.T, name string, selectedRef string, providerConfigs ...providerConfigSpec) endpointintent.Endpoint {
	t.Helper()

	parsedName, err := endpointintent.ParseEndpointName(name)
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	selected, err := endpointintent.ParseProviderConfigRef(selectedRef)
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	encodedProviderConfigs := make([]endpointintent.ProviderConfig, 0, len(providerConfigs))
	for _, spec := range providerConfigs {
		ref, err := endpointintent.ParseProviderConfigRef(spec.ref)
		if err != nil {
			t.Fatalf("ParseProviderConfigRef returned error: %v", err)
		}
		providerSpec, err := endpointintent.ParseProviderSpec(spec.providerSpec)
		if err != nil {
			t.Fatalf("ParseProviderSpec returned error: %v", err)
		}
		providerConfig, err := endpointintent.NewProviderConfig(ref, providerSpec, spec.baseURL, spec.credential, spec.protocol)
		if err != nil {
			t.Fatalf("NewProviderConfig returned error: %v", err)
		}
		encodedProviderConfigs = append(encodedProviderConfigs, providerConfig)
	}
	endpoint, err := endpointintent.NewEndpoint(parsedName, encodedProviderConfigs, selected)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}
