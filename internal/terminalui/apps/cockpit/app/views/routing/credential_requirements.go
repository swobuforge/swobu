package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func providerCredentialSelectionRequired(provider, baseURL, credentialRef string) bool {
	if strings.TrimSpace(provider) == "" {
		return false
	}
	if strings.TrimSpace(credentialRef) != "" {
		return true
	}
	return state.ProviderRequiresCredential(provider, baseURL)
}

func providerModelCatalogLoadBlocked(provider, baseURL, credentialRef string) bool {
	if !providerCredentialSelectionRequired(provider, baseURL, credentialRef) {
		return false
	}
	ref := strings.TrimSpace(credentialRef)
	if ref == "" {
		return true
	}
	source := credentialSource(ref)
	if strings.EqualFold(source, "file") && strings.TrimSpace(credentialFilePath(ref)) == "" {
		return true
	}
	return false
}
