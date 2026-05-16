package credentials

import "strings"

type CredentialWritePolicy string

const (
	CredentialWritePolicyKeyring CredentialWritePolicy = "keyring"
	CredentialWritePolicyFile    CredentialWritePolicy = "file"
	CredentialWritePolicyAuto    CredentialWritePolicy = "auto"
)

func NormalizeCredentialWritePolicy(raw string) CredentialWritePolicy {
	switch strings.TrimSpace(strings.ToLower(raw)) { // trimlowerlint:allow boundary canonicalization
	case "", "default":
		return CredentialWritePolicyAuto
	case string(CredentialWritePolicyFile):
		return CredentialWritePolicyFile
	case string(CredentialWritePolicyAuto):
		return CredentialWritePolicyAuto
	case string(CredentialWritePolicyKeyring):
		return CredentialWritePolicyKeyring
	default:
		return CredentialWritePolicyAuto
	}
}
