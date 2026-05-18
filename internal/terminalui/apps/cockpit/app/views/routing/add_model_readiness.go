package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func isEmptyFileCredentialRef(ref string) bool {
	trimmed := strings.TrimSpace(ref) // swobu:io-string source=boundary
	if strings.EqualFold(trimmed, "file") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(trimmed), fileCredentialRefPrefix) && strings.TrimSpace(credentialFilePath(trimmed)) == "" // swobu:io-string source=boundary
}

func addModelCreateReady(draft state.ProviderConfigSnapshot) bool {
	requiresInteractiveAuth := false
	for _, variant := range providercatalog.SupportedAuthVariantsForSpec(strings.TrimSpace(draft.ProviderSpec)) { // swobu:io-string source=boundary
		if providercatalog.IsInteractiveAuthVariant(variant) {
			requiresInteractiveAuth = true
			break
		}
	}
	return strings.TrimSpace(draft.ProviderSpec) != "" && // swobu:io-string source=boundary
		strings.TrimSpace(draft.ModelID) != "" && // swobu:io-string source=boundary
		(requiresInteractiveAuth || !providerCredentialSelectionRequired(draft.ProviderSpec, draft.BaseURL, draft.CredentialRef) || strings.TrimSpace(draft.CredentialRef) != "") && // swobu:io-string source=boundary
		!isEmptyFileCredentialRef(draft.CredentialRef) &&
		(!strings.EqualFold(strings.TrimSpace(draft.ProviderSpec), "openai_compatible") || strings.TrimSpace(draft.BaseURL) != "") // swobu:io-string source=boundary
}
