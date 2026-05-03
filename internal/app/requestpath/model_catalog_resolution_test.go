package requestpath

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func TestBuildEndpointModelCatalog_AssignsProgressiveIDs(t *testing.T) {
	endpoint := testEndpointForCatalog(t, []catalogProvider{
		{ref: "a", spec: "openai", model: "gpt-5.3"},
		{ref: "b", spec: "anthropic", model: "gpt-5.3"},
		{ref: "c", spec: "openai", model: "gpt-5.3"},
	}, "a")

	catalog := buildEndpointModelCatalog(endpoint)
	if len(catalog.Entries) != 3 {
		t.Fatalf("entries len = %d, want 3", len(catalog.Entries))
	}
	if got := catalog.Entries[0].ID; got != "anthropic:gpt-5.3" {
		t.Fatalf("entry[0] id = %q, want %q", got, "anthropic:gpt-5.3")
	}
	if got := catalog.Entries[1].ID; got != "openai:gpt-5.3:a" {
		t.Fatalf("entry[1] id = %q, want %q", got, "openai:gpt-5.3:a")
	}
	if got := catalog.Entries[2].ID; got != "openai:gpt-5.3:c" {
		t.Fatalf("entry[2] id = %q, want %q", got, "openai:gpt-5.3:c")
	}
}

func TestResolveProviderConfigForRequest_SelectorRules(t *testing.T) {
	endpoint := testEndpointForCatalog(t, []catalogProvider{
		{ref: "backend-a", spec: "custom", model: "gpt-5.3", alias: "deep"},
		{ref: "backend-b", spec: "custom", model: "gpt-4.1-mini"},
	}, "backend-a")
	catalog := buildEndpointModelCatalog(endpoint)

	t.Run("missing selector uses primary", func(t *testing.T) {
		cfg, mode, err := resolveProviderConfigForRequest(endpoint, catalog, "")
		if err != nil {
			t.Fatalf("resolve returned error: %v", err)
		}
		if got := cfg.Ref().String(); got != "backend-a" {
			t.Fatalf("ref = %q, want %q", got, "backend-a")
		}
		if got := mode; got != modelResolutionDefaultMissing {
			t.Fatalf("mode = %q, want %q", got, modelResolutionDefaultMissing)
		}
	})

	t.Run("primary selector uses selected target", func(t *testing.T) {
		cfg, mode, err := resolveProviderConfigForRequest(endpoint, catalog, strings.ToUpper(compatibility.PrimaryTargetSelector))
		if err != nil {
			t.Fatalf("resolve returned error: %v", err)
		}
		if got := cfg.Ref().String(); got != "backend-a" {
			t.Fatalf("ref = %q, want %q", got, "backend-a")
		}
		if got := mode; got != modelResolutionDefaultPrimary {
			t.Fatalf("mode = %q, want %q", got, modelResolutionDefaultPrimary)
		}
	})

	t.Run("alias selector resolves first", func(t *testing.T) {
		cfg, mode, err := resolveProviderConfigForRequest(endpoint, catalog, "deep")
		if err != nil {
			t.Fatalf("resolve returned error: %v", err)
		}
		if got := cfg.Ref().String(); got != "backend-a" {
			t.Fatalf("ref = %q, want %q", got, "backend-a")
		}
		if got := mode; got != modelResolutionClient {
			t.Fatalf("mode = %q, want %q", got, modelResolutionClient)
		}
	})

	t.Run("mechanical selector resolves when no alias", func(t *testing.T) {
		cfg, mode, err := resolveProviderConfigForRequest(endpoint, catalog, "gpt-4.1-mini")
		if err != nil {
			t.Fatalf("resolve returned error: %v", err)
		}
		if got := cfg.Ref().String(); got != "backend-b" {
			t.Fatalf("ref = %q, want %q", got, "backend-b")
		}
		if got := mode; got != modelResolutionClient {
			t.Fatalf("mode = %q, want %q", got, modelResolutionClient)
		}
	})

	t.Run("mechanical selector resolves when alias also exists", func(t *testing.T) {
		cfg, mode, err := resolveProviderConfigForRequest(endpoint, catalog, "gpt-5.3")
		if err != nil {
			t.Fatalf("resolve returned error: %v", err)
		}
		if got := cfg.Ref().String(); got != "backend-a" {
			t.Fatalf("ref = %q, want %q", got, "backend-a")
		}
		if got := mode; got != modelResolutionClient {
			t.Fatalf("mode = %q, want %q", got, modelResolutionClient)
		}
	})

	t.Run("provider model literal resolves when unambiguous", func(t *testing.T) {
		cfg, mode, err := resolveProviderConfigForRequest(endpoint, catalog, "custom:gpt-5.3")
		if err != nil {
			t.Fatalf("resolve returned error: %v", err)
		}
		if got := cfg.Ref().String(); got != "backend-a" {
			t.Fatalf("ref = %q, want %q", got, "backend-a")
		}
		if got := mode; got != modelResolutionClient {
			t.Fatalf("mode = %q, want %q", got, modelResolutionClient)
		}
	})

	t.Run("unknown explicit selector falls back to primary", func(t *testing.T) {
		cfg, mode, err := resolveProviderConfigForRequest(endpoint, catalog, "unknown")
		if err != nil {
			t.Fatalf("resolve returned error: %v", err)
		}
		if got := cfg.Ref().String(); got != "backend-a" {
			t.Fatalf("ref = %q, want %q", got, "backend-a")
		}
		if got := mode; got != modelResolutionDefaultUnknown {
			t.Fatalf("mode = %q, want %q", got, modelResolutionDefaultUnknown)
		}
	})
}

type catalogProvider struct {
	ref   string
	spec  string
	model string
	alias string
}

func testEndpointForCatalog(t *testing.T, providers []catalogProvider, selectedRef string) endpointintent.Endpoint {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	selected, err := endpointintent.ParseProviderConfigRef(selectedRef)
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	configs := make([]endpointintent.ProviderConfig, 0, len(providers))
	for _, provider := range providers {
		ref, err := endpointintent.ParseProviderConfigRef(provider.ref)
		if err != nil {
			t.Fatalf("ParseProviderConfigRef returned error: %v", err)
		}
		spec, err := endpointintent.ParseProviderSpec(provider.spec)
		if err != nil {
			t.Fatalf("ParseProviderSpec returned error: %v", err)
		}
		protocolKind := protocolsurface.ChatCompletions
		if def, ok := providercatalog.DefaultProtocolForSpec(provider.spec); ok {
			protocolKind = def
		}
		cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "", protocolKind)
		if err != nil {
			t.Fatalf("NewProviderConfig returned error: %v", err)
		}
		cfg, err = cfg.WithModelID(provider.model)
		if err != nil {
			t.Fatalf("WithModelID returned error: %v", err)
		}
		cfg, err = cfg.WithTargetAlias(provider.alias)
		if err != nil {
			t.Fatalf("WithTargetAlias returned error: %v", err)
		}
		configs = append(configs, cfg)
	}
	endpoint, err := endpointintent.NewEndpoint(name, configs, selected)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}
