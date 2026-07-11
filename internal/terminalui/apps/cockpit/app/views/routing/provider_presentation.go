package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
)

func providerDisplayName(spec string) string {
	profile, ok := profileForSpec(spec)
	if !ok {
		return "Provider"
	}
	return profile.ProviderDisplayName
}

func authVariantDisplayLabel(variant profile.AuthVariant) string {
	switch variant {
	case profile.AuthVariantChatGPTLogin:
		return "browser login"
	case profile.AuthVariantChatGPTDeviceAuth:
		return "device code"
	case profile.AuthVariantEnv:
		return "env var"
	case profile.AuthVariantFile:
		return "file"
	case profile.AuthVariantAWSProfile:
		return "AWS chain"
	case profile.AuthVariantAWSEnvSession:
		return "AWS chain"
	default:
		return string(variant)
	}
}

func authVariantStartAction(spec string, variant profile.AuthVariant) (label string, verb string, ok bool) {
	if !profile.SupportsAuthVariant(strings.TrimSpace(spec), variant) || !profile.IsInteractiveAuthVariant(variant) { // swobu:io-string source=boundary
		return "", "", false
	}
	switch variant {
	case profile.AuthVariantChatGPTDeviceAuth:
		return "start device auth", "start", true
	case profile.AuthVariantChatGPTLogin:
		return "start login", "login", true
	default:
		return "start login", "login", true
	}
}

func profileForSpec(spec string) (profile.Profile, bool) {
	providerID, ok := profile.ParseProviderID(spec)
	if !ok {
		return profile.Profile{}, false
	}
	for _, profile := range profile.All() {
		if profile.ProviderID == providerID {
			return profile, true
		}
	}
	return profile.Profile{}, false
}
