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
	spec := strings.TrimSpace(providerSpec)
	name := strings.TrimSpace(keyName)
	token := strings.TrimSpace(secret)
	if spec == "" {
		return fmt.Errorf("provider spec is required")
	}
	if name == "" {
		return fmt.Errorf("keychain key name is required")
	}
	if token == "" {
		return fmt.Errorf("keychain key value is required")
	}
	if err := keyringSet(KeyringScopeForProvider(spec), name, token); err != nil {
		setMemoryFallbackSecret(spec, name, token)
		slog.Warn("keychain unavailable, falling back to in-memory credential storage",
			"component", "credentials",
			"provider_spec", strings.ToLower(spec),
			"credential_slot", name,
		)
		return nil
	}
	return nil
}
