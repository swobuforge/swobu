package credentials

import (
	"context"
	"fmt"
	"strings"
	"time"

	keyringcommodity "github.com/zalando/go-keyring"
)

const keychainCredentialRefPrefix = "keychain:"
const keyringNamespace = "swobu"
const keyringLookupTimeout = 500 * time.Millisecond

// KeyringClient is the commodity seam used for OS keyring lookups.
type KeyringClient interface {
	Get(scope, user string) (string, error)
}

// OSKeyringClient is the production keyring commodity adapter.
type OSKeyringClient struct{}

func (OSKeyringClient) Get(scope, user string) (string, error) {
	return keyringcommodity.Get(scope, user)
}

// KeyringResolver resolves keychain credential refs against OS keyring.
type KeyringResolver struct {
	client KeyringClient
}

func NewKeyringResolver(client KeyringClient) KeyringResolver {
	if client == nil {
		client = OSKeyringClient{}
	}
	return KeyringResolver{client: client}
}

func (r KeyringResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	_ = ctx
	ref := strings.TrimSpace(credentialRef)
	keyName, parseErr := keychainCredentialName(providerSpec, ref)
	if parseErr != nil {
		return "", parseErr
	}
	scope := KeyringScopeForProvider(providerSpec)
	type lookupResult struct {
		token string
		err   error
	}
	ch := make(chan lookupResult, 1)
	go func() {
		token, err := r.client.Get(scope, keyName)
		ch <- lookupResult{token: token, err: err}
	}()
	var token string
	var err error
	select {
	case result := <-ch:
		token = result.token
		err = result.err
	case <-time.After(keyringLookupTimeout):
		return "", fmt.Errorf("keyring lookup timed out for %q", keyName)
	}
	if err != nil {
		return "", fmt.Errorf("keyring lookup failed for %q", keyName)
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("keyring token for %q is empty", keyName)
	}
	return token, nil
}

func KeyringScopeForProvider(providerSpec string) string {
	spec := strings.TrimSpace(strings.ToLower(providerSpec))
	if spec == "" {
		spec = "provider"
	}
	return keyringNamespace + "/" + spec
}

func CanonicalKeychainCredentialName(providerSpec string) string {
	spec := strings.TrimSpace(strings.ToLower(providerSpec))
	if spec == "" {
		return "default"
	}
	return spec + "/default"
}

func keychainCredentialName(providerSpec, credentialRef string) (string, error) {
	ref := strings.TrimSpace(credentialRef)
	if ref == "keychain" {
		return CanonicalKeychainCredentialName(providerSpec), nil
	}
	if !strings.HasPrefix(strings.ToLower(ref), keychainCredentialRefPrefix) {
		return "", fmt.Errorf("keyring resolver does not support credential ref %q", ref)
	}
	name := strings.TrimSpace(ref[len(keychainCredentialRefPrefix):])
	if name == "" {
		return "", fmt.Errorf("keychain credential name must not be empty")
	}
	return name, nil
}
