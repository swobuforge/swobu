package routing

import "strings"

const fileCredentialRefPrefix = "file:"
const keychainCredentialRefPrefix = "keychain:"
const envCredentialRefPrefix = "env:"

func credentialSource(credentialRef string) string {
	ref := strings.TrimSpace(credentialRef) // trimlowerlint:allow boundary canonicalization
	switch ref {
	case "env", "keychain", "file":
		return ref
	}
	if strings.HasPrefix(strings.ToLower(ref), envCredentialRefPrefix) { // trimlowerlint:allow boundary canonicalization
		return "env"
	}
	if strings.HasPrefix(strings.ToLower(ref), keychainCredentialRefPrefix) { // trimlowerlint:allow boundary canonicalization
		return "keychain"
	}
	if strings.HasPrefix(strings.ToLower(ref), fileCredentialRefPrefix) { // trimlowerlint:allow boundary canonicalization
		return "file"
	}
	return ref
}

func credentialFilePath(credentialRef string) string {
	ref := strings.TrimSpace(credentialRef)                               // trimlowerlint:allow boundary canonicalization
	if strings.HasPrefix(strings.ToLower(ref), fileCredentialRefPrefix) { // trimlowerlint:allow boundary canonicalization
		return strings.TrimSpace(ref[len(fileCredentialRefPrefix):]) // trimlowerlint:allow boundary canonicalization
	}
	if strings.HasPrefix(ref, "~/") || strings.HasPrefix(ref, "/") {
		return ref
	}
	return ""
}

func encodeCredentialFileRef(path string) string {
	trimmed := strings.TrimSpace(path) // trimlowerlint:allow boundary canonicalization
	if trimmed == "" {
		return "file"
	}
	return fileCredentialRefPrefix + trimmed
}

func envCredentialKey(credentialRef string) string {
	ref := strings.TrimSpace(credentialRef)                               // trimlowerlint:allow boundary canonicalization
	if !strings.HasPrefix(strings.ToLower(ref), envCredentialRefPrefix) { // trimlowerlint:allow boundary canonicalization
		return ""
	}
	return strings.TrimSpace(ref[len(envCredentialRefPrefix):]) // trimlowerlint:allow boundary canonicalization
}

func encodeCredentialEnvRef(key string) string {
	trimmed := strings.TrimSpace(key) // trimlowerlint:allow boundary canonicalization
	if trimmed == "" {
		return "env"
	}
	return envCredentialRefPrefix + trimmed
}

func keychainCredentialName(credentialRef string) string {
	ref := strings.TrimSpace(credentialRef)                                    // trimlowerlint:allow boundary canonicalization
	if !strings.HasPrefix(strings.ToLower(ref), keychainCredentialRefPrefix) { // trimlowerlint:allow boundary canonicalization
		return ""
	}
	return strings.TrimSpace(ref[len(keychainCredentialRefPrefix):]) // trimlowerlint:allow boundary canonicalization
}

func encodeCredentialKeychainRef(name string) string {
	trimmed := strings.TrimSpace(name) // trimlowerlint:allow boundary canonicalization
	if trimmed == "" {
		return "keychain"
	}
	return keychainCredentialRefPrefix + trimmed
}
