package credentials

import (
	"fmt"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
	keyringcommodity "github.com/zalando/go-keyring"
)

var keyringSet = keyringcommodity.Set
var keyringGet = keyringcommodity.Get

func StoreMaterializedCredential(providerSpec string, keyName string, secret string, policy CredentialWritePolicy) (string, error) {
	scope, err := credentialref.NewProviderScope(providerSpec)
	if err != nil {
		return "", fmt.Errorf("provider spec is required")
	}
	return storeMaterializedCredential(scope, keyName, secret, policy)
}

func storeMaterializedCredential(scope credentialref.Scope, keyName string, secret string, policy CredentialWritePolicy) (string, error) {
	if !scope.Valid() {
		return "", fmt.Errorf("credential scope is required")
	}
	name := strings.TrimSpace(keyName) // swobu:io-string source=boundary
	token := strings.TrimSpace(secret) // swobu:io-string source=boundary
	if scope.Kind() == credentialref.ScopeConnection {
		if !validConnectionSlot(scope, name) {
			return "", fmt.Errorf("credential is outside Connection scope")
		}
	} else if strings.HasPrefix(name, "connections/") {
		return "", fmt.Errorf("Connection credentials require Connection scope")
	}
	if name == "" {
		return "", fmt.Errorf("stored secret name is required")
	}
	if token == "" {
		return "", fmt.Errorf("stored secret value is required")
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
	selectedPolicy, err := ParseCredentialWritePolicy(string(policy))
	if err != nil {
		return "", err
	}
	if selectedPolicy == CredentialWritePolicyFile {
		if err := (&secretFileStore{}).Store(name, encoded); err != nil {
			return "", err
		}
		return "secretfile:" + name, nil
	}
	if err := keyringSet(KeyringScopeForCredential(scope), name, encoded); err != nil {
		return "", fmt.Errorf("keyring write failed for %q: %w", name, err)
	}
	return "secret:" + name, nil
}

func StoreSecretByRef(providerSpec string, credentialRef string, secret string) error {
	name, kind, err := parseStoredSecretRef(providerSpec, credentialRef)
	if err != nil {
		return err
	}
	if kind == "secret" {
		if err := keyringSet(KeyringScopeForProvider(providerSpec), name, secret); err != nil {
			return fmt.Errorf("keyring write failed for %q: %w", name, err)
		}
		return nil
	}
	if kind == "secretfile" {
		return (&secretFileStore{}).Store(name, secret)
	}
	return fmt.Errorf("unsupported credential ref kind for refresh")
}

func ResolveStoredSecretByRef(providerSpec string, credentialRef string) (string, error) {
	name, kind, err := parseStoredSecretRef(providerSpec, credentialRef)
	if err != nil {
		return "", err
	}
	if kind == "secret" {
		token, err := keyringGet(KeyringScopeForProvider(providerSpec), name)
		if err != nil {
			return "", fmt.Errorf("keyring lookup failed for %q: %w", name, err)
		}
		token = strings.TrimSpace(token) // swobu:io-string source=boundary
		if token == "" {
			return "", fmt.Errorf("keyring token for %q is empty", name)
		}
		return token, nil
	}
	if kind == "secretfile" {
		return (&secretFileStore{}).ResolveRaw(name)
	}
	return "", fmt.Errorf("unsupported credential ref kind for refresh")
}

func parseStoredSecretRef(providerSpec string, credentialRef string) (name string, kind string, err error) {
	ref := strings.TrimSpace(credentialRef)                                 // swobu:io-string source=boundary
	if strings.HasPrefix(strings.ToLower(ref), secretCredentialRefPrefix) { // swobu:io-string source=boundary
		name, err := secretCredentialName(providerSpec, ref)
		if err == nil && strings.HasPrefix(name, "connections/") {
			return "", "", fmt.Errorf("Connection credentials require Connection scope")
		}
		return name, "secret", err
	}
	if strings.HasPrefix(strings.ToLower(ref), secretFileCredentialRefPrefix) { // swobu:io-string source=boundary
		name, err := secretFileCredentialName(ref)
		if err == nil && strings.HasPrefix(name, "connections/") {
			return "", "", fmt.Errorf("Connection credentials require Connection scope")
		}
		return name, "secretfile", err
	}
	return "", "", fmt.Errorf("credential ref %q is not refreshable stored secret", ref)
}
