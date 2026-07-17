package runtime

import (
	"net/http"
	"sync"

	"github.com/swobuforge/swobu/internal/profile"
)

// RuntimeFactory constructs one provider runtime bundle for one provider ID.
type RuntimeFactory func(client *http.Client, credentials CredentialProvider) ProviderRuntimeBundle

var (
	runtimeFactoryMu sync.RWMutex
	runtimeFactories = make(map[profile.ProviderID]RuntimeFactory)
)

// RegisterRuntimeFactory installs one provider runtime factory at the provider
// namespace edge. Duplicate registrations are rejected because they would hide
// provider ownership collisions.
func RegisterRuntimeFactory(providerID profile.ProviderID, factory RuntimeFactory) {
	if providerID == "" || factory == nil {
		return
	}
	runtimeFactoryMu.Lock()
	defer runtimeFactoryMu.Unlock()
	if _, exists := runtimeFactories[providerID]; exists {
		panic("providersruntime: duplicate runtime factory registration for provider id " + string(providerID))
	}
	runtimeFactories[providerID] = factory
}

// RuntimeFactoryFor returns the registered runtime factory for one provider id.
func RuntimeFactoryFor(providerID profile.ProviderID) (RuntimeFactory, bool) {
	runtimeFactoryMu.RLock()
	defer runtimeFactoryMu.RUnlock()
	factory, ok := runtimeFactories[providerID]
	return factory, ok
}
