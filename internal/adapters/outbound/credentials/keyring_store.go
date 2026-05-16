package credentials

import (
	"fmt"
	"log/slog"
	"strings"

	keyringcommodity "github.com/zalando/go-keyring"
)

var keyringSet = keyringcommodity.Set

// StoreKeychainCredential writes a provider-scoped keychain secret.
func StoreKeychainCredential(providerSpec string, keyName string, secret string) error {
	_, err := StoreMaterializedCredential(providerSpec, keyName, secret, CredentialWritePolicyKeyring)
	return err
}

func StoreMaterializedCredential(providerSpec string, keyName string, secret string, policy CredentialWritePolicy) (string, error) {
	spec := strings.TrimSpace(providerSpec) // trimlowerlint:allow boundary canonicalization
	name := strings.TrimSpace(keyName)      // trimlowerlint:allow boundary canonicalization
	token := strings.TrimSpace(secret)      // trimlowerlint:allow boundary canonicalization
	if spec == "" {
		return "", fmt.Errorf("provider spec is required")
	}
	if name == "" {
		return "", fmt.Errorf("keychain key name is required")
	}
	if token == "" {
		return "", fmt.Errorf("keychain key value is required")
	}
	selectedPolicy := NormalizeCredentialWritePolicy(string(policy))
	switch selectedPolicy {
	case CredentialWritePolicyFile:
		if err := (&secretFileStore{}).Store(name, token); err != nil {
			return "", err
		}
		return "secretfile:" + name, nil
	case CredentialWritePolicyAuto:
		if err := keyringSet(KeyringScopeForProvider(spec), name, token); err == nil {
			return "secret:" + name, nil
		} else {
			slog.Warn("keyring write failed, falling back to credential file store",
				"component", "credentials",
				"provider_spec", strings.ToLower(spec), // trimlowerlint:allow boundary canonicalization
				"credential_slot", name,
				"write_policy", string(selectedPolicy),
				"error", err.Error(),
			)
		}
		if err := (&secretFileStore{}).Store(name, token); err != nil {
			return "", err
		}
		return "secretfile:" + name, nil
	default:
		if err := keyringSet(KeyringScopeForProvider(spec), name, token); err != nil {
			return "", fmt.Errorf("keyring write failed for %q: %w", name, err)
		}
		return "secret:" + name, nil
	}
}
