package profile

import "strings"

// BedrockProfileNameFromCredentialRef extracts the profile name from a
// canonical Bedrock profile credential ref such as profile:work-prod.
// Optional @region suffixes are stripped because region already has a typed
// Bedrock seam.
func BedrockProfileNameFromCredentialRef(raw string) string {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(ref), "profile:") {
		return ""
	}
	return bedrockProfileNameFromInput(ref[len("profile:"):])
}

// BedrockProfileNameFromInput normalizes raw Bedrock profile input from the
// cockpit row. It accepts either a bare profile name or a profile: ref and
// strips any optional @region suffix.
func BedrockProfileNameFromInput(raw string) string {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(ref), "profile:") {
		ref = strings.TrimSpace(ref[len("profile:"):])
	}
	return bedrockProfileNameFromInput(ref)
}

// BedrockProfileCredentialRef canonicalizes one Bedrock profile name into the
// durable profile: prefix used by the runtime adapter.
func BedrockProfileCredentialRef(name string) string {
	profileName := BedrockProfileNameFromInput(name)
	if profileName == "" {
		return ""
	}
	return "profile:" + profileName
}

func bedrockProfileNameFromInput(raw string) string {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return ""
	}
	if name, _, found := strings.Cut(ref, "@"); found {
		ref = strings.TrimSpace(name)
	}
	return ref
}
