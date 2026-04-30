package routing

import "strings"

const fileCredentialRefPrefix = "file:"
const keychainCredentialRefPrefix = "keychain:"
const envCredentialRefPrefix = "env:"

func credentialSource(credentialRef string) string {
	ref := strings.TrimSpace(credentialRef)
	switch ref {
	case "env", "keychain", "file":
		return ref
	}
	if strings.HasPrefix(strings.ToLower(ref), envCredentialRefPrefix) {
		return "env"
	}
	if strings.HasPrefix(strings.ToLower(ref), keychainCredentialRefPrefix) {
		return "keychain"
	}
	if strings.HasPrefix(strings.ToLower(ref), fileCredentialRefPrefix) {
		return "file"
	}
	return ref
}

func credentialFilePath(credentialRef string) string {
	ref := strings.TrimSpace(credentialRef)
	if strings.HasPrefix(strings.ToLower(ref), fileCredentialRefPrefix) {
		return strings.TrimSpace(ref[len(fileCredentialRefPrefix):])
	}
	if strings.HasPrefix(ref, "~/") || strings.HasPrefix(ref, "/") {
		return ref
	}
	return ""
}

func encodeCredentialFileRef(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "file"
	}
	return fileCredentialRefPrefix + trimmed
}

func envCredentialKey(credentialRef string) string {
	ref := strings.TrimSpace(credentialRef)
	if !strings.HasPrefix(strings.ToLower(ref), envCredentialRefPrefix) {
		return ""
	}
	return strings.TrimSpace(ref[len(envCredentialRefPrefix):])
}

func encodeCredentialEnvRef(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "env"
	}
	return envCredentialRefPrefix + trimmed
}

func keychainCredentialName(credentialRef string) string {
	ref := strings.TrimSpace(credentialRef)
	if !strings.HasPrefix(strings.ToLower(ref), keychainCredentialRefPrefix) {
		return ""
	}
	return strings.TrimSpace(ref[len(keychainCredentialRefPrefix):])
}

func encodeCredentialKeychainRef(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "keychain"
	}
	return keychainCredentialRefPrefix + trimmed
}
