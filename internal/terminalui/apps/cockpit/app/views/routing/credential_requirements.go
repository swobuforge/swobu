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
