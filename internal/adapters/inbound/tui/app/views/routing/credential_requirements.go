package routing

import (
	"strings"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state"
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
