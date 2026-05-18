package model

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

var modelCatalogAuthErrorMarkers = []string{
	"auth",
	"credential",
	"api key",
	"access token",
	"bearer token",
	"unauthorized",
	"forbidden",
	"expired",
	"login",
	"sign in",
}

func ProviderCredentialVariantIsInteractive(provider, credentialRef string) bool {
	ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if ref == "" {
		return false
	}
	variant := providercatalog.AuthVariant(strings.ToLower(credentialSource(ref)))  // swobu:io-string source=boundary
	if !providercatalog.SupportsAuthVariant(strings.TrimSpace(provider), variant) { // swobu:io-string source=boundary
		return false
	}
	return providercatalog.IsInteractiveAuthVariant(variant)
}

func ProviderCredentialSelectionRequired(provider, baseURL, credentialRef string) bool {
	if strings.TrimSpace(provider) == "" { // swobu:io-string source=boundary
		return false
	}
	interactiveRequired := false
	for _, variant := range providercatalog.SupportedAuthVariantsForSpec(strings.TrimSpace(provider)) { // swobu:io-string source=boundary
		if providercatalog.IsInteractiveAuthVariant(variant) {
			interactiveRequired = true
			break
		}
	}
	if interactiveRequired {
		ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
		return ref == "" || ProviderCredentialVariantIsInteractive(provider, ref)
	}
	if strings.TrimSpace(credentialRef) != "" { // swobu:io-string source=boundary
		return true
	}
	return ProviderRequiresCredential(provider, baseURL)
}

func ProviderModelCatalogLoadBlocked(provider, baseURL, credentialRef string) bool {
	if !ProviderCredentialSelectionRequired(provider, baseURL, credentialRef) {
		return false
	}
	ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if ref == "" {
		return true
	}
	if ProviderCredentialVariantIsInteractive(provider, ref) {
		return true
	}
	source := credentialSource(ref)
	if strings.EqualFold(source, "file") && strings.TrimSpace(fileCredentialPath(ref)) == "" { // swobu:io-string source=boundary
		return true
	}
	return false
}

func ProviderModelCatalogBlockedMessage(provider, baseURL, credentialRef string) string {
	if !ProviderModelCatalogLoadBlocked(provider, baseURL, credentialRef) {
		return ""
	}
	for _, variant := range providercatalog.SupportedAuthVariantsForSpec(strings.TrimSpace(provider)) { // swobu:io-string source=boundary
		if providercatalog.IsInteractiveAuthVariant(variant) {
			return ""
		}
	}
	return "set credential file before loading models"
}

func ProviderModelCatalogAuthFailed(probeError string) bool {
	errText := strings.TrimSpace(strings.ToLower(probeError)) // swobu:io-string source=boundary
	if errText == "" {
		return false
	}
	for _, marker := range modelCatalogAuthErrorMarkers {
		if strings.Contains(errText, marker) { // swobu:io-string source=boundary
			return true
		}
	}
	return false
}

func ProviderModelCatalogAuthFailureMessage(probeError string) string {
	trimmed := strings.TrimSpace(probeError) // swobu:io-string source=boundary
	if !ProviderModelCatalogAuthFailed(trimmed) {
		return ""
	}
	return trimmed
}

func credentialSource(credentialRef string) string {
	trimmed := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if trimmed == "" {
		return ""
	}
	if idx := strings.Index(trimmed, ":"); idx > 0 {
		return strings.ToLower(strings.TrimSpace(trimmed[:idx])) // swobu:io-string source=boundary
	}
	return strings.ToLower(trimmed) // swobu:io-string source=boundary
}

func fileCredentialPath(credentialRef string) string {
	trimmed := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if idx := strings.Index(trimmed, ":"); idx >= 0 {
		if idx+1 >= len(trimmed) {
			return ""
		}
		return strings.TrimSpace(trimmed[idx+1:]) // swobu:io-string source=boundary
	}
	return ""
}
