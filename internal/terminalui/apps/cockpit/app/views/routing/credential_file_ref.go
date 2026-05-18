package routing

import "strings"

const fileCredentialRefPrefix = "file:"
const keychainCredentialRefPrefix = "keychain:"
const envCredentialRefPrefix = "env:"

func credentialSource(credentialRef string) string {
	ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if ref == "env" || ref == "keychain" || ref == "file" {
		return ref
	}
	if strings.HasPrefix(strings.ToLower(ref), envCredentialRefPrefix) { // swobu:io-string source=boundary
		return "env"
	}
	if strings.HasPrefix(strings.ToLower(ref), keychainCredentialRefPrefix) { // swobu:io-string source=boundary
		return "keychain"
	}
	if strings.HasPrefix(strings.ToLower(ref), fileCredentialRefPrefix) { // swobu:io-string source=boundary
		return "file"
	}
	return ref
}

func credentialFilePath(credentialRef string) string {
	ref := strings.TrimSpace(credentialRef)                               // swobu:io-string source=boundary
	if strings.HasPrefix(strings.ToLower(ref), fileCredentialRefPrefix) { // swobu:io-string source=boundary
		return strings.TrimSpace(ref[len(fileCredentialRefPrefix):]) // swobu:io-string source=boundary
	}
	if strings.HasPrefix(ref, "~/") || strings.HasPrefix(ref, "/") {
		return ref
	}
	return ""
}

func encodeCredentialFileRef(path string) string {
	trimmed := strings.TrimSpace(path) // swobu:io-string source=boundary
	if trimmed == "" {
		return "file"
	}
	return fileCredentialRefPrefix + trimmed
}

func envCredentialKey(credentialRef string) string {
	ref := strings.TrimSpace(credentialRef)                               // swobu:io-string source=boundary
	if !strings.HasPrefix(strings.ToLower(ref), envCredentialRefPrefix) { // swobu:io-string source=boundary
		return ""
	}
	return strings.TrimSpace(ref[len(envCredentialRefPrefix):]) // swobu:io-string source=boundary
}

func encodeCredentialEnvRef(key string) string {
	trimmed := strings.TrimSpace(key) // swobu:io-string source=boundary
	if trimmed == "" {
		return "env"
	}
	return envCredentialRefPrefix + trimmed
}

func keychainCredentialName(credentialRef string) string {
	ref := strings.TrimSpace(credentialRef)                                    // swobu:io-string source=boundary
	if !strings.HasPrefix(strings.ToLower(ref), keychainCredentialRefPrefix) { // swobu:io-string source=boundary
		return ""
	}
	return strings.TrimSpace(ref[len(keychainCredentialRefPrefix):]) // swobu:io-string source=boundary
}

func encodeCredentialKeychainRef(name string) string {
	trimmed := strings.TrimSpace(name) // swobu:io-string source=boundary
	if trimmed == "" {
		return "keychain"
	}
	return keychainCredentialRefPrefix + trimmed
}
