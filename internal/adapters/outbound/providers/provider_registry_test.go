package providers

import (
	"context"
	"net/http"
	"testing"

	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

func mustProviderRegistry(t *testing.T, client *http.Client, credentials providersruntime.CredentialProvider) ProviderRegistry {
	t.Helper()
	registry, err := NewProviderRegistry(client, credentials)
	if err != nil {
		t.Fatalf("NewProviderRegistry: %v", err)
	}
	return registry
}

func TestProviderRegistry_BuildsFacetRegistriesForSupportedSpecs(t *testing.T) {
	t.Parallel()

	registry := mustProviderRegistry(t, http.DefaultClient, testCredentialResolver{})
	for _, spec := range profile.SupportedSpecs() {
		providerID, ok := profile.ParseProviderID(spec)
		if !ok {
			t.Fatalf("supported spec %q did not parse as provider id", spec)
		}
		manifest, ok := registry.Manifest(providerID)
		if !ok || manifest.ProviderID != providerID {
			t.Fatalf("manifest lookup failed for %q", spec)
		}
		if resolver, ok := registry.BackendResolver(providerID); !ok || resolver == nil {
			t.Fatalf("backend resolver lookup failed for %q", spec)
		}
		if discovery, ok := registry.Discovery(providerID); !ok || discovery == nil {
			t.Fatalf("discovery lookup failed for %q", spec)
		}
	}
}

func TestProviderRegistryIsExplicitlyConstructed(t *testing.T) {
	registry := mustProviderRegistry(t, http.DefaultClient, testCredentialResolver{})
	if len(registry.backends) != len(profile.All()) {
		t.Fatalf("explicit provider count = %d, want %d", len(registry.backends), len(profile.All()))
	}
}

func TestMissingProviderFailsAtStartup(t *testing.T) {
	_, err := newProviderRegistry(profile.All(), nil)
	if err == nil {
		t.Fatal("missing fixed provider runtimes must fail composition")
	}
}

func TestProviderRegistry_RejectsUnknownProviderID(t *testing.T) {
	t.Parallel()

	registry := mustProviderRegistry(t, http.DefaultClient, testCredentialResolver{})
	if _, ok := registry.Manifest("unknown-provider"); ok {
		t.Fatal("unknown provider manifest must be absent")
	}
	if _, ok := registry.BackendResolver("unknown-provider"); ok {
		t.Fatal("unknown provider backend resolver must be absent")
	}
	if _, ok := registry.Discovery("unknown-provider"); ok {
		t.Fatal("unknown provider discovery must be absent")
	}
	if _, err := registry.ProbeTarget(context.Background(), provider.TargetSnapshot{}); err == nil {
		t.Fatal("unknown provider id must fail")
	}
}

func TestProviderBackendMatchesCandidateTarget(t *testing.T) {
	registry := mustProviderRegistry(t, http.DefaultClient, testCredentialResolver{})
	target := provider.NewTargetSnapshot("backend-a", "openai", "https://api.openai.com/v1", "credential-a", protocolkind.Responses, "", "responses")
	target.Model = "gpt-4.1-mini"
	backend, err := registry.ResolveBackend(target)
	if err != nil {
		t.Fatalf("ResolveBackend: %v", err)
	}
	if !backend.Target.Equal(target) {
		t.Fatalf("backend target = %#v, want %#v", backend.Target, target)
	}
}

func TestAdvertisedProviderProtocolsDeclareToolDiscoveryAuthority(t *testing.T) {
	registry := mustProviderRegistry(t, http.DefaultClient, testCredentialResolver{})
	expected := map[string]provider.ToolDiscoveryMode{
		"ollama/chat_completions": provider.ToolDiscoveryPolyfill, "ollama/chat_completions_stream": provider.ToolDiscoveryPolyfill,
		"openai/responses": provider.ToolDiscoveryNative, "openai/responses_stream": provider.ToolDiscoveryNative,
		"openai/chat_completions": provider.ToolDiscoveryPolyfill, "openai/chat_completions_stream": provider.ToolDiscoveryPolyfill,
		"chatgpt/responses_stream": provider.ToolDiscoveryPolyfill,
		"anthropic/messages":       provider.ToolDiscoveryNative, "anthropic/messages_stream": provider.ToolDiscoveryNative,
		"deepseek/messages_stream": provider.ToolDiscoveryPolyfill,
		"openrouter/responses":     provider.ToolDiscoveryPolyfill, "openrouter/responses_stream": provider.ToolDiscoveryPolyfill,
		"openrouter/chat_completions": provider.ToolDiscoveryPolyfill, "openrouter/chat_completions_stream": provider.ToolDiscoveryPolyfill,
		"zai/chat_completions_stream": provider.ToolDiscoveryPolyfill,
		"bedrock/responses":           provider.ToolDiscoveryPolyfill, "bedrock/responses_stream": provider.ToolDiscoveryPolyfill,
		"bedrock/chat_completions": provider.ToolDiscoveryPolyfill, "bedrock/chat_completions_stream": provider.ToolDiscoveryPolyfill,
		"bedrock/messages": provider.ToolDiscoveryPolyfill, "bedrock/messages_stream": provider.ToolDiscoveryPolyfill,
		"azure/responses": provider.ToolDiscoveryNative, "azure/responses_stream": provider.ToolDiscoveryNative,
		"azure/chat_completions": provider.ToolDiscoveryPolyfill, "azure/chat_completions_stream": provider.ToolDiscoveryPolyfill,
		"azure/messages": provider.ToolDiscoveryPolyfill, "azure/messages_stream": provider.ToolDiscoveryPolyfill,
		"custom/responses": provider.ToolDiscoveryPolyfill, "custom/responses_stream": provider.ToolDiscoveryPolyfill,
		"custom/chat_completions": provider.ToolDiscoveryPolyfill, "custom/chat_completions_stream": provider.ToolDiscoveryPolyfill,
		"custom/messages": provider.ToolDiscoveryPolyfill, "custom/messages_stream": provider.ToolDiscoveryPolyfill,
	}
	seen := make(map[string]struct{}, len(expected))
	for _, manifest := range profile.All() {
		resolver, ok := registry.BackendResolver(manifest.ProviderID)
		if !ok {
			t.Fatalf("provider %q has no backend resolver", manifest.ProviderID)
		}
		for _, protocol := range manifest.ProviderProtocols {
			key := string(manifest.ProviderID) + "/" + protocol.Name
			want, ok := expected[key]
			if !ok {
				t.Fatalf("advertised provider protocol %q has no expected tool-discovery authority", key)
			}
			target := advertisedProtocolTarget(manifest.ProviderID, protocol)
			backend, err := resolver.ResolveBackend(target)
			if err != nil {
				t.Fatalf("ResolveBackend(%s): %v", key, err)
			}
			if backend.ToolDiscovery == provider.ToolDiscoveryUnspecified || backend.ToolDiscovery != want {
				t.Fatalf("%s tool discovery = %v, want %v", key, backend.ToolDiscovery, want)
			}
			seen[key] = struct{}{}
		}
	}
	for key := range expected {
		if _, ok := seen[key]; !ok {
			t.Fatalf("tool-discovery expectation %q no longer matches an advertised provider protocol", key)
		}
	}
}

func advertisedProtocolTarget(providerID profile.ProviderID, protocol profile.ProviderProtocolSpec) provider.TargetSnapshot {
	const credentialRef = "credential"
	var target provider.TargetSnapshot
	switch providerID {
	case profile.ProviderSpecBedrock:
		target = provider.NewBedrockTargetSnapshot("target", "https://bedrock-mantle.eu-west-2.api.aws", credentialRef, protocol.Kind, protocol.Frame, protocol.Name, "eu-west-2")
	case profile.ProviderSpecCustom:
		target = provider.NewCustomTargetSnapshot("target", "https://example.test/v1", credentialRef, protocol.Kind, protocol.Frame, protocol.Name, "Authorization")
	case profile.ProviderSpecAzure:
		target = provider.NewTargetSnapshot("target", string(providerID), "https://example.openai.azure.com", credentialRef, protocol.Kind, protocol.Frame, protocol.Name)
	default:
		target = provider.NewTargetSnapshot("target", string(providerID), "https://example.test/v1", credentialRef, protocol.Kind, protocol.Frame, protocol.Name)
	}
	target.Model = "model"
	return target
}
