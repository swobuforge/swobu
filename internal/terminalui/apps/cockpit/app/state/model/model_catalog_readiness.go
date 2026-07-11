package model

import (
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
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
	variant := profile.AuthVariant(strings.ToLower(credentialSource(ref)))  // swobu:io-string source=boundary
	if !profile.SupportsAuthVariant(strings.TrimSpace(provider), variant) { // swobu:io-string source=boundary
		return false
	}
	return profile.IsInteractiveAuthVariant(variant)
}

func ProviderCredentialSelectionRequired(provider, baseURL, credentialRef string) bool {
	if strings.TrimSpace(provider) == "" { // swobu:io-string source=boundary
		return false
	}
	interactiveRequired := false
	for _, variant := range profile.SupportedAuthVariantsForSpec(strings.TrimSpace(provider)) { // swobu:io-string source=boundary
		if profile.IsInteractiveAuthVariant(variant) {
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
	for _, variant := range profile.SupportedAuthVariantsForSpec(strings.TrimSpace(provider)) { // swobu:io-string source=boundary
		if profile.IsInteractiveAuthVariant(variant) {
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

func ProviderModelCatalogAuthFailureMessage(provider string, credentialRef string, probeError string) string {
	trimmed := strings.TrimSpace(probeError) // swobu:io-string source=boundary
	if !ProviderModelCatalogAuthFailed(trimmed) {
		return ""
	}
	normalized := strings.TrimSpace(strings.TrimPrefix(trimmed, "BAD_ENDPOINT:")) // swobu:io-string source=boundary
	if strings.EqualFold(normalized, "chatgpt subscription tier could not be resolved from credential") {
		if strings.EqualFold(strings.TrimSpace(provider), "chatgpt") && // swobu:io-string source=boundary
			!ProviderCredentialVariantIsInteractive(provider, credentialRef) &&
			strings.TrimSpace(credentialRef) != "" { // swobu:io-string source=boundary
			return "signed-in account could not resolve ChatGPT subscription tier; sign in another account"
		}
		return "sign in to resolve ChatGPT subscription tier"
	}
	return normalized
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
