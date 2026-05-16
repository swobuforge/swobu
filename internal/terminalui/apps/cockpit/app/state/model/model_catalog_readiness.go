package model

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func ProviderCredentialVariantIsInteractive(provider, credentialRef string) bool {
	ref := strings.TrimSpace(credentialRef) // trimlowerlint:allow boundary canonicalization
	if ref == "" {
		return false
	}
	variant := providercatalog.AuthVariant(strings.ToLower(credentialSource(ref)))  // trimlowerlint:allow boundary canonicalization
	if !providercatalog.SupportsAuthVariant(strings.TrimSpace(provider), variant) { // trimlowerlint:allow boundary canonicalization
		return false
	}
	return providercatalog.IsInteractiveAuthVariant(variant)
}

func ProviderCredentialSelectionRequired(provider, baseURL, credentialRef string) bool {
	if strings.TrimSpace(provider) == "" { // trimlowerlint:allow boundary canonicalization
		return false
	}
	interactiveRequired := false
	for _, variant := range providercatalog.SupportedAuthVariantsForSpec(strings.TrimSpace(provider)) { // trimlowerlint:allow boundary canonicalization
		if providercatalog.IsInteractiveAuthVariant(variant) {
			interactiveRequired = true
			break
		}
	}
	if interactiveRequired {
		ref := strings.TrimSpace(credentialRef) // trimlowerlint:allow boundary canonicalization
		return ref == "" || ProviderCredentialVariantIsInteractive(provider, ref)
	}
	if strings.TrimSpace(credentialRef) != "" { // trimlowerlint:allow boundary canonicalization
		return true
	}
	return ProviderRequiresCredential(provider, baseURL)
}

func ProviderModelCatalogLoadBlocked(provider, baseURL, credentialRef string) bool {
	if !ProviderCredentialSelectionRequired(provider, baseURL, credentialRef) {
		return false
	}
	ref := strings.TrimSpace(credentialRef) // trimlowerlint:allow boundary canonicalization
	if ref == "" {
		return true
	}
	if ProviderCredentialVariantIsInteractive(provider, ref) {
		return true
	}
	source := credentialSource(ref)
	if strings.EqualFold(source, "file") && strings.TrimSpace(fileCredentialPath(ref)) == "" { // trimlowerlint:allow boundary canonicalization
		return true
	}
	return false
}

func ProviderModelCatalogBlockedMessage(provider, baseURL, credentialRef string) string {
	if !ProviderModelCatalogLoadBlocked(provider, baseURL, credentialRef) {
		return ""
	}
	for _, variant := range providercatalog.SupportedAuthVariantsForSpec(strings.TrimSpace(provider)) { // trimlowerlint:allow boundary canonicalization
		if providercatalog.IsInteractiveAuthVariant(variant) {
			return ""
		}
	}
	return "set credential file before loading models"
}

func credentialSource(credentialRef string) string {
	trimmed := strings.TrimSpace(credentialRef) // trimlowerlint:allow boundary canonicalization
	if trimmed == "" {
		return ""
	}
	if idx := strings.Index(trimmed, ":"); idx > 0 {
		return strings.ToLower(strings.TrimSpace(trimmed[:idx])) // trimlowerlint:allow boundary canonicalization
	}
	return strings.ToLower(trimmed) // trimlowerlint:allow boundary canonicalization
}

func fileCredentialPath(credentialRef string) string {
	trimmed := strings.TrimSpace(credentialRef) // trimlowerlint:allow boundary canonicalization
	if idx := strings.Index(trimmed, ":"); idx >= 0 {
		if idx+1 >= len(trimmed) {
			return ""
		}
		return strings.TrimSpace(trimmed[idx+1:]) // trimlowerlint:allow boundary canonicalization
	}
	return ""
}
