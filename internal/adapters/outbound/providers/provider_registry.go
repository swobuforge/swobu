package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/anthropic"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/azure"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/bedrock"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/cerebras"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/chatgpt"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/deepinfra"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/deepseek"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/fireworks"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/friendli"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/gemini"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/gmi"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/groq"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/kimi"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/llm7"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/mistral"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/novita"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openai"
	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openrouter"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/runpod"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/sambanova"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/scaleway"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/siliconflow"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/together"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/workersai"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/zai"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// ProviderRegistry is the outbound provider namespace registry and dispatch root.
type ProviderRegistry struct {
	manifests   map[profile.ProviderID]profile.Profile
	discoveries map[profile.ProviderID]providersruntime.Discovery
	backends    map[profile.ProviderID]provider.BackendResolver
	support     map[profile.ProviderID]provider.TargetSupportResolver
}

// NewProviderRegistry explicitly constructs the fixed provider set. Provider
// availability is determined here, never by package initialization order.
func NewProviderRegistry(client *http.Client, credentials providersruntime.CredentialProvider) (ProviderRegistry, error) {
	if client == nil {
		client = http.DefaultClient
	}
	return newProviderRegistry(profile.All(), []providersruntime.ProviderRuntimeBundle{
		openai.NewRuntime(client, credentials),
		anthropic.NewRuntime(client, credentials),
		cerebras.NewRuntime(client, credentials),
		deepseek.NewRuntime(client, credentials),
		kimi.NewRuntime(client, credentials),
		llm7.NewRuntime(client, credentials),
		mistral.NewRuntime(client, credentials),
		novita.NewRuntime(client, credentials),
		friendli.NewRuntime(client, credentials),
		gemini.NewRuntime(client, credentials),
		gmi.NewRuntime(client, credentials),
		groq.NewRuntime(client, credentials),
		fireworks.NewRuntime(client, credentials),
		together.NewRuntime(client, credentials),
		workersai.NewRuntime(client, credentials),
		deepinfra.NewRuntime(client, credentials),
		scaleway.NewRuntime(client, credentials),
		sambanova.NewRuntime(client, credentials),
		openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecStepFun)),
		chatgpt.NewRuntime(profile.ProviderSpecChatGPT, client, credentials),
		bedrock.NewRuntime(profile.ProviderSpecBedrock, client, credentials),
		azure.NewRuntime(client, credentials),
		openrouter.NewRuntime(client, credentials),
		runpod.NewRuntime(client, credentials),
		zai.NewRuntime(client, credentials),
		openaifamily.NewRuntime(client, credentials, openaifamily.StandardNoAuthPolicy(profile.ProviderSpecOllama)),
		openaifamily.NewRuntime(client, credentials, openaifamily.LMStudioPolicy()),
		openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecVLLM)),
		openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecCustom)),
		openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecNebius)),
		openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecNVIDIA)),
		// Baseten intentionally composes the shared standard runtime in P0; its
		// managed and exact deployment bases do not require a provider codec.
		openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecBaseten)),
		// Hyperbolic's manual model authoring is derived from its profile while
		// the inference runtime remains the shared Chat transport.
		openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecHyperbolic)),
		siliconflow.NewRuntime(client, credentials),
	})
}

func newProviderRegistry(manifests []profile.Profile, runtimes []providersruntime.ProviderRuntimeBundle) (ProviderRegistry, error) {
	manifestByID := make(map[profile.ProviderID]profile.Profile, len(manifests))
	for _, manifest := range manifests {
		if manifest.ProviderID == "" {
			return ProviderRegistry{}, fmt.Errorf("providers: manifest provider id is empty")
		}
		manifestByID[manifest.ProviderID] = manifest
	}
	discoveryByID := make(map[profile.ProviderID]providersruntime.Discovery, len(runtimes))
	backendByID := make(map[profile.ProviderID]provider.BackendResolver, len(runtimes))
	supportByID := make(map[profile.ProviderID]provider.TargetSupportResolver, len(runtimes))
	for _, runtime := range runtimes {
		if runtime.ProviderID == "" || runtime.Discovery == nil || runtime.BackendResolver == nil || runtime.TargetSupport == nil {
			return ProviderRegistry{}, fmt.Errorf("providers: runtime bundle is incomplete")
		}
		if _, exists := backendByID[runtime.ProviderID]; exists {
			return ProviderRegistry{}, fmt.Errorf("providers: duplicate runtime for provider id %q", runtime.ProviderID)
		}
		discoveryByID[runtime.ProviderID] = runtime.Discovery
		backendByID[runtime.ProviderID] = runtime.BackendResolver
		supportByID[runtime.ProviderID] = runtime.TargetSupport
	}
	for providerID := range manifestByID {
		if backendByID[providerID] == nil || discoveryByID[providerID] == nil || supportByID[providerID] == nil {
			return ProviderRegistry{}, fmt.Errorf("providers: missing runtime for provider id %q", providerID)
		}
	}
	for providerID := range backendByID {
		if _, exists := manifestByID[providerID]; !exists {
			return ProviderRegistry{}, fmt.Errorf("providers: runtime has no manifest for provider id %q", providerID)
		}
	}
	return ProviderRegistry{
		manifests:   cloneManifestRegistry(manifestByID),
		discoveries: cloneDiscoveryRegistry(discoveryByID),
		backends:    cloneBackendRegistry(backendByID),
		support:     cloneTargetSupportRegistry(supportByID),
	}, nil
}

// ResolveBackend resolves the selected target through exactly one fixed
// provider implementation and rejects any target drift.
func (r ProviderRegistry) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	providerID, err := providerIDFromTarget(target.ProviderID())
	if err != nil {
		return provider.Backend{}, err
	}
	resolver, ok := r.BackendResolver(providerID)
	if !ok {
		return provider.Backend{}, canonical.InternalError("provider backend resolver is missing")
	}
	backend, err := resolver.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	if !backend.Target.Equal(target) {
		return provider.Backend{}, canonical.InternalError("provider backend target does not match selected target")
	}
	return backend, nil
}

// ResolveTargetSupport dispatches exact-target knowledge to the provider-owned
// runtime facet. The registry contains no capability matrix of its own.
func (r ProviderRegistry) ResolveTargetSupport(target provider.TargetSnapshot) provider.TargetSupport {
	providerID, err := providerIDFromTarget(target.ProviderID())
	if err != nil {
		return provider.TargetSupport{}
	}
	resolver, ok := r.TargetSupportResolver(providerID)
	if !ok {
		return provider.TargetSupport{}
	}
	return resolver.ResolveTargetSupport(target)
}

func (r ProviderRegistry) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	providerID, err := providerIDFromTarget(target.ProviderID())
	if err != nil {
		return provider.TargetProbeResult{}, err
	}
	discovery, ok := r.Discovery(providerID)
	if !ok {
		return provider.TargetProbeResult{}, canonical.InternalError("provider discovery facet is missing")
	}
	return discovery.ProbeTarget(ctx, target)
}

func (r ProviderRegistry) Manifest(providerID profile.ProviderID) (profile.Profile, bool) {
	if r.manifests == nil {
		return profile.Profile{}, false
	}
	manifest, ok := r.manifests[providerID]
	return manifest, ok
}

func (r ProviderRegistry) Discovery(providerID profile.ProviderID) (providersruntime.Discovery, bool) {
	if r.discoveries == nil {
		return nil, false
	}
	discovery, ok := r.discoveries[providerID]
	if !ok || discovery == nil {
		return nil, false
	}
	return discovery, true
}

func (r ProviderRegistry) BackendResolver(providerID profile.ProviderID) (provider.BackendResolver, bool) {
	if r.backends == nil {
		return nil, false
	}
	resolver, ok := r.backends[providerID]
	if !ok || resolver == nil {
		return nil, false
	}
	return resolver, true
}

func (r ProviderRegistry) TargetSupportResolver(providerID profile.ProviderID) (provider.TargetSupportResolver, bool) {
	if r.support == nil {
		return nil, false
	}
	resolver, ok := r.support[providerID]
	if !ok || resolver == nil {
		return nil, false
	}
	return resolver, true
}

func providerIDFromTarget(rawProviderID string) (profile.ProviderID, error) {
	providerID, ok := profile.ParseProviderID(rawProviderID)
	if !ok {
		return "", canonical.BadEndpoint("provider id is unsupported")
	}
	return providerID, nil
}

func cloneManifestRegistry(src map[profile.ProviderID]profile.Profile) map[profile.ProviderID]profile.Profile {
	if len(src) == 0 {
		return map[profile.ProviderID]profile.Profile{}
	}
	out := make(map[profile.ProviderID]profile.Profile, len(src))
	for providerID, manifest := range src {
		out[providerID] = manifest
	}
	return out
}

func cloneDiscoveryRegistry(src map[profile.ProviderID]providersruntime.Discovery) map[profile.ProviderID]providersruntime.Discovery {
	if len(src) == 0 {
		return map[profile.ProviderID]providersruntime.Discovery{}
	}
	out := make(map[profile.ProviderID]providersruntime.Discovery, len(src))
	for providerID, discovery := range src {
		out[providerID] = discovery
	}
	return out
}

func cloneBackendRegistry(src map[profile.ProviderID]provider.BackendResolver) map[profile.ProviderID]provider.BackendResolver {
	if len(src) == 0 {
		return map[profile.ProviderID]provider.BackendResolver{}
	}
	out := make(map[profile.ProviderID]provider.BackendResolver, len(src))
	for providerID, resolver := range src {
		out[providerID] = resolver
	}
	return out
}

func cloneTargetSupportRegistry(src map[profile.ProviderID]provider.TargetSupportResolver) map[profile.ProviderID]provider.TargetSupportResolver {
	if len(src) == 0 {
		return map[profile.ProviderID]provider.TargetSupportResolver{}
	}
	out := make(map[profile.ProviderID]provider.TargetSupportResolver, len(src))
	for providerID, resolver := range src {
		out[providerID] = resolver
	}
	return out
}

var _ provider.BackendResolver = ProviderRegistry{}
var _ provider.TargetSupportResolver = ProviderRegistry{}
var _ provider.Discovery = ProviderRegistry{}
