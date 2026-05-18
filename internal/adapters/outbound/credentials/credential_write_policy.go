package credentials

import "strings"

type CredentialWritePolicy string

const (
	CredentialWritePolicyKeyring CredentialWritePolicy = "keyring"
	CredentialWritePolicyFile    CredentialWritePolicy = "file"
	CredentialWritePolicyAuto    CredentialWritePolicy = "auto"
)

func NormalizeCredentialWritePolicy(raw string) CredentialWritePolicy {
	normalized := strings.TrimSpace(strings.ToLower(raw)) // swobu:io-string source=boundary
	if normalized == "" || normalized == "default" {
		return CredentialWritePolicyAuto
	}
	if normalized == string(CredentialWritePolicyFile) {
		return CredentialWritePolicyFile
	}
	if normalized == string(CredentialWritePolicyAuto) {
		return CredentialWritePolicyAuto
	}
	if normalized == string(CredentialWritePolicyKeyring) {
		return CredentialWritePolicyKeyring
	}
	return CredentialWritePolicyAuto
}
