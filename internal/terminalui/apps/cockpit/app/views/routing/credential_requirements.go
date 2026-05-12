package routing

import (
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func providerCredentialSelectionRequired(provider, baseURL, credentialRef string) bool {
	return state.ProviderCredentialSelectionRequired(provider, baseURL, credentialRef)
}

func providerModelCatalogLoadBlocked(provider, baseURL, credentialRef string) bool {
	return state.ProviderModelCatalogLoadBlocked(provider, baseURL, credentialRef)
}

func providerModelCatalogBlockedMessage(provider, baseURL, credentialRef string) string {
	return state.ProviderModelCatalogBlockedMessage(provider, baseURL, credentialRef)
}
