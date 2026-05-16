package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func providerDisplayName(spec string) string {
	profile, ok := profileForSpec(spec)
	if !ok {
		return "Provider"
	}
	return profile.ProviderDisplayName
}

func authVariantDisplayLabel(variant providercatalog.AuthVariant) string {
	switch variant {
	case providercatalog.AuthVariantChatGPTLogin:
		return "browser login"
	case providercatalog.AuthVariantChatGPTDeviceAuth:
		return "device code"
	case providercatalog.AuthVariantEnv:
		return "env var"
	case providercatalog.AuthVariantFile:
		return "file"
	default:
		return string(variant)
	}
}

func authVariantStartAction(spec string, variant providercatalog.AuthVariant) (label string, verb string, ok bool) {
	if !providercatalog.SupportsAuthVariant(strings.TrimSpace(spec), variant) || !providercatalog.IsInteractiveAuthVariant(variant) { // trimlowerlint:allow boundary canonicalization
		return "", "", false
	}
	switch variant {
	case providercatalog.AuthVariantChatGPTDeviceAuth:
		return "start device auth", "start", true
	case providercatalog.AuthVariantChatGPTLogin:
		return "start login", "login", true
	default:
		return "start login", "login", true
	}
}

func profileForSpec(spec string) (providercatalog.Profile, bool) {
	providerID, ok := providercatalog.ParseProviderID(spec)
	if !ok {
		return providercatalog.Profile{}, false
	}
	for _, profile := range providercatalog.All() {
		if profile.ProviderID == providerID {
			return profile, true
		}
	}
	return providercatalog.Profile{}, false
}
