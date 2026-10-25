package credentials

import (
	"fmt"
	"strings"
)

type CredentialWritePolicy string

const (
	CredentialWritePolicyKeyring CredentialWritePolicy = "keyring"
	CredentialWritePolicyFile    CredentialWritePolicy = "file"
)

func ParseCredentialWritePolicy(raw string) (CredentialWritePolicy, error) {
	normalized := strings.TrimSpace(strings.ToLower(raw)) // swobu:io-string source=boundary
	switch normalized {
	case "", "default", string(CredentialWritePolicyFile):
		return CredentialWritePolicyFile, nil
	case string(CredentialWritePolicyKeyring):
		return CredentialWritePolicyKeyring, nil
	default:
		return "", fmt.Errorf("unsupported credential write policy %q: use file or keyring", raw)
	}
}
