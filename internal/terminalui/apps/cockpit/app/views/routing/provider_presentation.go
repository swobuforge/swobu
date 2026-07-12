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

func authModeDisplayLabel(mode profile.AuthMode) string {
	switch mode {
	case profile.AuthModeChatGPTLogin:
		return "browser login"
	case profile.AuthModeChatGPTDeviceAuth:
		return "device code"
	case profile.AuthModeEnv:
		return "env var"
	case profile.AuthModeFile:
		return "file"
	case profile.AuthModeKeychain:
		return "paste raw"
	case profile.AuthModeAWSProfile:
		return "AWS chain"
	case profile.AuthModeAWSEnvSession:
		return "AWS chain"
	default:
		return string(mode)
	}
}

func authModeStartAction(spec string, mode profile.AuthMode) (label string, verb string, ok bool) {
	if !profile.SupportsAuthMode(strings.TrimSpace(spec), mode) || !profile.IsInteractiveAuthMode(mode) { // swobu:io-string source=boundary
		return "", "", false
	}
	switch mode {
	case profile.AuthModeChatGPTDeviceAuth:
		return "start device auth", "start", true
	case profile.AuthModeChatGPTLogin:
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
