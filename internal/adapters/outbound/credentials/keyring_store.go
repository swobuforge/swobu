package credentials

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	keyringcommodity "github.com/zalando/go-keyring"
)

var keyringSet = keyringcommodity.Set
var keyringGet = keyringcommodity.Get

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
	encoded := token
	if _, _, decodeErr := DecodeTokenBundle(token); decodeErr != nil {
		wrapped, wrapErr := EncodeTokenBundle(TokenBundle{
			AccessToken: token,
			IssuedAt:    time.Now().UTC(),
		})
		if wrapErr != nil {
			return "", wrapErr
		}
		encoded = wrapped
	}
	selectedPolicy := NormalizeCredentialWritePolicy(string(policy))
	switch selectedPolicy {
	case CredentialWritePolicyFile:
		if err := (&secretFileStore{}).Store(name, encoded); err != nil {
			return "", err
		}
		return "secretfile:" + name, nil
	case CredentialWritePolicyAuto:
		if err := keyringSet(KeyringScopeForProvider(spec), name, encoded); err == nil {
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
		if err := (&secretFileStore{}).Store(name, encoded); err != nil {
			return "", err
		}
		return "secretfile:" + name, nil
	default:
		if err := keyringSet(KeyringScopeForProvider(spec), name, encoded); err != nil {
			return "", fmt.Errorf("keyring write failed for %q: %w", name, err)
		}
		return "secret:" + name, nil
	}
}

func StoreSecretByRef(providerSpec string, credentialRef string, secret string) error {
	name, kind, err := parseStoredSecretRef(providerSpec, credentialRef)
	if err != nil {
		return err
	}
	switch kind {
	case "secret":
		if err := keyringSet(KeyringScopeForProvider(providerSpec), name, secret); err != nil {
			return fmt.Errorf("keyring write failed for %q: %w", name, err)
		}
		return nil
	case "secretfile":
		return (&secretFileStore{}).Store(name, secret)
	default:
		return fmt.Errorf("unsupported credential ref kind for refresh")
	}
}

func ResolveStoredSecretByRef(providerSpec string, credentialRef string) (string, error) {
	name, kind, err := parseStoredSecretRef(providerSpec, credentialRef)
	if err != nil {
		return "", err
	}
	switch kind {
	case "secret":
		token, err := keyringGet(KeyringScopeForProvider(providerSpec), name)
		if err != nil {
			return "", fmt.Errorf("keyring lookup failed for %q: %w", name, err)
		}
		token = strings.TrimSpace(token) // trimlowerlint:allow boundary canonicalization
		if token == "" {
			return "", fmt.Errorf("keyring token for %q is empty", name)
		}
		return token, nil
	case "secretfile":
		return (&secretFileStore{}).ResolveRaw(name)
	default:
		return "", fmt.Errorf("unsupported credential ref kind for refresh")
	}
}

func parseStoredSecretRef(providerSpec string, credentialRef string) (name string, kind string, err error) {
	ref := strings.TrimSpace(credentialRef)                                 // trimlowerlint:allow boundary canonicalization
	if strings.HasPrefix(strings.ToLower(ref), secretCredentialRefPrefix) { // trimlowerlint:allow boundary canonicalization
		name, err := secretCredentialName(providerSpec, ref)
		return name, "secret", err
	}
	if strings.HasPrefix(strings.ToLower(ref), secretFileCredentialRefPrefix) { // trimlowerlint:allow boundary canonicalization
		name, err := secretFileCredentialName(ref)
		return name, "secretfile", err
	}
	return "", "", fmt.Errorf("credential ref %q is not refreshable stored secret", ref)
}
