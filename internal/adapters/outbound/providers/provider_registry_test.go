package providers

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	openaiadapter "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openai"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/cachelocality"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/thread"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/testkit/providertest"
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

func TestProviderRegistryRejectsRuntimeWithoutBackendFacet(t *testing.T) {
	var manifest profile.Profile
	for _, candidate := range profile.All() {
		if candidate.ProviderID == profile.ProviderSpecOpenAI {
			manifest = candidate
			break
		}
	}
	runtime := openaiadapter.NewRuntime(http.DefaultClient, testCredentialResolver{})
	runtime.BackendResolver = nil
	_, err := newProviderRegistry([]profile.Profile{manifest}, []providersruntime.ProviderRuntimeBundle{runtime})
	if err == nil {
		t.Fatal("runtime without backend facet must fail composition")
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
	target := provider.NewTargetSnapshot("backend-a", "openai", "https://api.openai.com/v1", "credential-a", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	target.Model = "gpt-4.1-mini"
	backend, err := registry.ResolveBackend(target)
	if err != nil {
		t.Fatalf("ResolveBackend: %v", err)
	}
	if !backend.Target.Equal(target) {
		t.Fatalf("backend target = %#v, want %#v", backend.Target, target)
	}
}

func TestAdvertisedProviderCodecsAreTotalWithValidLossForCanonicalBasis(t *testing.T) {
	registry := mustProviderRegistry(t, http.DefaultClient, testCredentialResolver{})
	basis := canonicaltest.ProjectionBasis(t, "model")
	for _, witness := range basis {
		t.Run(witness.Name, func(t *testing.T) {
			assertAdvertisedProviderProjection(t, registry, witness.Request)
		})
	}
}

// assertAdvertisedProviderProjection uses the production registry as the sole
// provider membership authority. It proves total definedness, valid loss
// evidence, and cache-stable rendering; owning codecs prove structural
// conservation for the relations they encode.
func assertAdvertisedProviderProjection(t *testing.T, registry ProviderRegistry, request canonical.CanonicalRequest) {
	t.Helper()
	threadID, err := thread.Derive("provider-registry-test/v1", "canonical-basis")
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range profile.All() {
		for _, protocol := range manifest.ProviderProtocols {
			name := string(manifest.ProviderID) + "/" + protocol.Name
			t.Run(name, func(t *testing.T) {
				target := advertisedProtocolTarget(manifest.ProviderID, protocol)
				backend, err := registry.ResolveBackend(target)
				if err != nil {
					t.Fatalf("resolve advertised backend: %v", err)
				}
				project := func(exchangeID, locality string, mode delivery.Delivery) []byte {
					names, nameChanges, err := provider.BuildAttemptToolNames(request)
					if err != nil {
						t.Fatalf("build attempt tool names: %v", err)
					}
					document, changes, err := backend.Codec.Encode(provider.Request{
						Attempt: provider.AttemptContext{ExchangeID: exchangeID, CacheLocality: cachelocality.Explicit(locality), ThreadID: threadID}, Canonical: request,
						Delivery: mode, ToolNames: names,
						EncodeContext: provider.EncodeContext{Context: context.Background(), ResolveImage: func(context.Context, canonical.URLImage) (provider.InspectedImage, error) {
							return provider.InspectedImage{MediaType: canonical.ImageMediaPNG, Bytes: []byte("PNG"), Width: 1, Height: 1}, nil
						}},
					})
					if err != nil {
						t.Fatalf("encode advertised backend: %v", err)
					}
					if err := compat.ValidateChanges(append(nameChanges, changes...)); err != nil {
						t.Fatalf("invalid semantic-loss evidence: %v", err)
					}
					projection, err := providertest.CacheSensitiveProjection(document)
					if err != nil {
						t.Fatalf("project advertised backend: %v", err)
					}
					return projection
				}
				first := project("exchange-a", "locality-a", protocol.Delivery)
				repeated := project("exchange-b", "locality-b", protocol.Delivery)
				if !bytes.Equal(first, repeated) {
					t.Fatalf("cache-sensitive projection changed: first=%s repeated=%s", first, repeated)
				}
				if alternate, ok := alternateAdvertisedDelivery(manifest, protocol); ok {
					alternateProjection := project("exchange-c", "locality-c", alternate)
					if !bytes.Equal(first, alternateProjection) {
						t.Fatalf("cache-sensitive projection changed across delivery: first=%s alternate=%s", first, alternateProjection)
					}
				}
			})
		}
	}
}

func advertisedProtocolTarget(providerID profile.ProviderID, protocol profile.ProviderProtocolSpec) provider.TargetSnapshot {
	const credentialRef = "credential"
	var target provider.TargetSnapshot
	switch providerID {
	case profile.ProviderSpecBedrock:
		target = provider.NewBedrockTargetSnapshot("target", "https://bedrock-mantle.eu-west-2.api.aws", credentialRef, protocol.Kind, protocol.Name, "eu-west-2", protocol.Delivery)
	case profile.ProviderSpecCustom:
		target = provider.NewCustomTargetSnapshot("target", "https://example.test/v1", credentialRef, protocol.Kind, protocol.Name, "Authorization", protocol.Delivery)
	case profile.ProviderSpecAzure:
		target = provider.NewTargetSnapshot("target", string(providerID), "https://example.openai.azure.com", credentialRef, protocol.Kind, protocol.Name, protocol.Delivery)
	default:
		target = provider.NewTargetSnapshot("target", string(providerID), "https://example.test/v1", credentialRef, protocol.Kind, protocol.Name, protocol.Delivery)
	}
	target.Model = "model"
	if providerID == profile.ProviderSpecWorkersAI {
		target.Model = "@cf/example/model"
	}
	return target
}

func alternateAdvertisedDelivery(manifest profile.Profile, protocol profile.ProviderProtocolSpec) (delivery.Delivery, bool) {
	for _, candidate := range manifest.ProviderProtocols {
		if candidate.Kind == protocol.Kind && candidate.Delivery != protocol.Delivery {
			return candidate.Delivery, true
		}
	}
	return delivery.Delivery{}, false
}
