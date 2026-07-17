package providers

import (
	"context"
	"net/http"

	_ "github.com/swobuforge/swobu/internal/adapters/outbound/providers/anthropic"
	_ "github.com/swobuforge/swobu/internal/adapters/outbound/providers/azure"
	_ "github.com/swobuforge/swobu/internal/adapters/outbound/providers/bedrock"
	_ "github.com/swobuforge/swobu/internal/adapters/outbound/providers/chatgpt"
	_ "github.com/swobuforge/swobu/internal/adapters/outbound/providers/ollama"
	_ "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openai"
	_ "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaicompat"
	_ "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openrouter"
	providercompat "github.com/swobuforge/swobu/internal/adapters/outbound/providers/providercompat"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
)

// ProviderRegistry is the outbound provider namespace registry and dispatch root.
type ProviderRegistry struct {
	manifests   map[profile.ProviderID]profile.Profile
	discoveries map[profile.ProviderID]providersruntime.Discovery
	ingresses   map[profile.ProviderID]providersruntime.ProviderExecutor
}

type providerRegistryBuilder struct {
	manifests   map[profile.ProviderID]profile.Profile
	discoveries map[profile.ProviderID]providersruntime.Discovery
	ingresses   map[profile.ProviderID]providersruntime.ProviderExecutor
}

// NewRegistryBuilder composes provider facets into a registry builder at the
// namespace edge.
func NewRegistryBuilder() providersruntime.Builder {
	return &providerRegistryBuilder{
		manifests:   make(map[profile.ProviderID]profile.Profile),
		discoveries: make(map[profile.ProviderID]providersruntime.Discovery),
		ingresses:   make(map[profile.ProviderID]providersruntime.ProviderExecutor),
	}
}

// NewProviderRegistry composes concrete provider adapters once at the
// composition edge.
func NewProviderRegistry(client *http.Client, credentials providersruntime.CredentialProvider) ProviderRegistry {
	if client == nil {
		client = http.DefaultClient
	}
	builder := NewRegistryBuilder().(*providerRegistryBuilder)
	for _, manifest := range profile.All() {
		builder.RegisterManifest(manifest)
		runtimeFactory, ok := providersruntime.RuntimeFactoryFor(manifest.ProviderID)
		if !ok {
			panic("providers: missing runtime constructor for provider id " + string(manifest.ProviderID))
		}
		runtime := runtimeFactory(client, credentials)
		builder.RegisterDiscovery(manifest.ProviderID, runtime.Discovery)
		builder.RegisterIngress(manifest.ProviderID, runtime.ProviderExecutor)
	}
	registry, ok := builder.Build().(ProviderRegistry)
	if !ok {
		panic("providers: registry builder returned unexpected registry type")
	}
	return registry
}

func (b *providerRegistryBuilder) RegisterManifest(manifest profile.Profile) {
	if b == nil || manifest.ProviderID == "" {
		return
	}
	b.manifests[manifest.ProviderID] = manifest
}

func (b *providerRegistryBuilder) RegisterDiscovery(providerID profile.ProviderID, discovery providersruntime.Discovery) {
	if b == nil || providerID == "" || discovery == nil {
		return
	}
	b.discoveries[providerID] = discovery
}

func (b *providerRegistryBuilder) RegisterIngress(providerID profile.ProviderID, ingress providersruntime.ProviderExecutor) {
	if b == nil || providerID == "" || ingress == nil {
		return
	}
	b.ingresses[providerID] = ingress
}

func (b *providerRegistryBuilder) Build() providersruntime.Registry {
	if b == nil {
		return ProviderRegistry{}
	}
	return ProviderRegistry{
		manifests:   cloneManifestRegistry(b.manifests),
		discoveries: cloneDiscoveryRegistry(b.discoveries),
		ingresses:   cloneIngressRegistry(b.ingresses),
	}
}

func (r ProviderRegistry) ResolveProviderIngress(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
	providerID, err := providerIDFromTarget(req.Target.ProviderID())
	if err != nil {
		return nil, err
	}
	ingress, ok := r.Ingress(providerID)
	if !ok {
		return nil, canonical.InternalError("provider ingress facet is missing")
	}
	if err := providercompat.GateRouteFeatureSupport(ctx, req.EffectSink, req.ExchangeID, string(providerID), string(req.Target.ProtocolKind), req.Request); err != nil {
		return nil, err
	}
	return ingress.ResolveProviderIngress(ctx, req)
}

func (r ProviderRegistry) ListDeployments(ctx context.Context, target exchange.RoutableTarget) ([]profile.ProviderDeploymentRecord, error) {
	providerID, err := providerIDFromTarget(target.ProviderID())
	if err != nil {
		return nil, err
	}
	discovery, ok := r.Discovery(providerID)
	if !ok {
		return nil, canonical.InternalError("provider discovery facet is missing")
	}
	return discovery.ListDeployments(ctx, target)
}

func (r ProviderRegistry) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	providerID, err := providerIDFromTarget(target.ProviderID())
	if err != nil {
		return err
	}
	discovery, ok := r.Discovery(providerID)
	if !ok {
		return canonical.InternalError("provider discovery facet is missing")
	}
	return discovery.ValidateCredentials(ctx, target)
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

func (r ProviderRegistry) Ingress(providerID profile.ProviderID) (providersruntime.ProviderExecutor, bool) {
	if r.ingresses == nil {
		return nil, false
	}
	ingress, ok := r.ingresses[providerID]
	if !ok || ingress == nil {
		return nil, false
	}
	return ingress, true
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

func cloneIngressRegistry(src map[profile.ProviderID]providersruntime.ProviderExecutor) map[profile.ProviderID]providersruntime.ProviderExecutor {
	if len(src) == 0 {
		return map[profile.ProviderID]providersruntime.ProviderExecutor{}
	}
	out := make(map[profile.ProviderID]providersruntime.ProviderExecutor, len(src))
	for providerID, ingress := range src {
		out[providerID] = ingress
	}
	return out
}

var _ exchange.ProviderIngressResolver = ProviderRegistry{}
var _ exchange.ProviderModelCatalog = ProviderRegistry{}
var _ providersruntime.Registry = ProviderRegistry{}
