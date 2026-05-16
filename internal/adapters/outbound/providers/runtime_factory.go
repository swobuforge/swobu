package providers

import (
	"fmt"
	"net/http"

	anthropicprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/anthropic"
	chatgptprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/chatgpt"
	openaicompatprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaicompat"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

// RuntimeFactory composes concrete per-provider runtime strategies from domain registry entries.
type RuntimeFactory struct {
	client             *http.Client
	credentialProvider providersruntime.CredentialProvider
}

func NewRuntimeFactory(client *http.Client, credentialProvider providersruntime.CredentialProvider) RuntimeFactory {
	if client == nil {
		client = http.DefaultClient
	}
	return RuntimeFactory{client: client, credentialProvider: credentialProvider}
}

func (f RuntimeFactory) Build(registry []providercatalog.Profile) map[providercatalog.ProviderID]providersruntime.ProviderRuntime {
	byProviderID := make(map[providercatalog.ProviderID]providersruntime.ProviderRuntime, len(registry))
	for _, profile := range registry {
		providerID := profile.ProviderID
		if providerID == "" {
			panic("providers: empty provider id in registry entry")
		}
		if _, exists := byProviderID[providerID]; exists {
			panic("providers: duplicate provider runtime for provider id " + string(providerID))
		}
		runtime := f.runtimeFor(providerID)
		validateRuntimeAgainstProfile(profile, runtime)
		byProviderID[providerID] = runtime
	}
	return byProviderID
}

func (f RuntimeFactory) runtimeFor(providerID providercatalog.ProviderID) providersruntime.ProviderRuntime {
	switch providerID {
	case providercatalog.ProviderSpecOllama,
		providercatalog.ProviderSpecOpenAI,
		providercatalog.ProviderSpecOpenRouter,
		providercatalog.ProviderSpecOpenAICompatible:
		return openaicompatprovider.NewRuntime(providerID, f.client, f.credentialProvider)
	case providercatalog.ProviderSpecAnthropic:
		return anthropicprovider.NewRuntime(providerID, f.client, f.credentialProvider)
	case providercatalog.ProviderSpecChatGPT:
		return chatgptprovider.NewRuntime(providerID, f.client, f.credentialProvider)
	default:
		panic("providers: missing runtime constructor for provider id " + string(providerID))
	}
}

func validateRuntimeAgainstProfile(profile providercatalog.Profile, runtime providersruntime.ProviderRuntime) {
	providerID := profile.ProviderID
	if runtime.ProviderID != providerID {
		panic(fmt.Sprintf("providers: runtime id mismatch for %s", providerID))
	}
	if runtime.Executor == nil {
		panic(fmt.Sprintf("providers: missing executor for provider id %s", providerID))
	}
	if runtime.CredentialProvider == nil {
		panic(fmt.Sprintf("providers: missing credential provider for provider id %s", providerID))
	}
	if providercatalog.SupportsCapability(string(providerID), providercatalog.CapabilityModelCatalog) && runtime.ModelCatalogClient == nil {
		panic(fmt.Sprintf("providers: model catalog capability declared without ModelCatalogClient for provider id %s", providerID))
	}
}
