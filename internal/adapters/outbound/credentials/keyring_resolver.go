package credentials

import (
	"context"
	"fmt"
	"strings"
	"time"

	keyringcommodity "github.com/zalando/go-keyring"
)

const keychainCredentialRefPrefix = "keychain:"
const secretCredentialRefPrefix = "secret:"
const secretFileCredentialRefPrefix = "secretfile:"
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
	ref := strings.TrimSpace(credentialRef) // trimlowerlint:allow boundary canonicalization
	keyName, parseErr := secretCredentialName(providerSpec, ref)
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
		return "", fmt.Errorf("keyring lookup failed for %q: %w", keyName, err)
	}
	if strings.TrimSpace(token) == "" { // trimlowerlint:allow boundary canonicalization
		return "", fmt.Errorf("keyring token for %q is empty", keyName)
	}
	bundle, _, err := DecodeTokenBundle(token)
	if err != nil {
		return "", fmt.Errorf("keyring token for %q is invalid: %w", keyName, err)
	}
	return bundle.AccessToken, nil
}

func KeyringScopeForProvider(providerSpec string) string {
	spec := strings.TrimSpace(strings.ToLower(providerSpec)) // trimlowerlint:allow boundary canonicalization
	if spec == "" {
		spec = "provider"
	}
	return keyringNamespace + "/" + spec
}

func CanonicalKeychainCredentialName(providerSpec string) string {
	spec := strings.TrimSpace(strings.ToLower(providerSpec)) // trimlowerlint:allow boundary canonicalization
	if spec == "" {
		return "default"
	}
	return spec + "/default"
}

func keychainCredentialName(providerSpec, credentialRef string) (string, error) {
	return secretCredentialName(providerSpec, credentialRef)
}

func secretCredentialName(providerSpec, credentialRef string) (string, error) {
	ref := strings.TrimSpace(credentialRef) // trimlowerlint:allow boundary canonicalization
	if ref == "keychain" || ref == "secret" {
		return CanonicalKeychainCredentialName(providerSpec), nil
	}
	if strings.HasPrefix(strings.ToLower(ref), secretCredentialRefPrefix) { // trimlowerlint:allow boundary canonicalization
		name := strings.TrimSpace(ref[len(secretCredentialRefPrefix):]) // trimlowerlint:allow boundary canonicalization
		if name == "" {
			return "", fmt.Errorf("secret credential name must not be empty")
		}
		return name, nil
	}
	if strings.HasPrefix(strings.ToLower(ref), keychainCredentialRefPrefix) { // trimlowerlint:allow boundary canonicalization
		name := strings.TrimSpace(ref[len(keychainCredentialRefPrefix):]) // trimlowerlint:allow boundary canonicalization
		if name == "" {
			return "", fmt.Errorf("keychain credential name must not be empty")
		}
		return name, nil
	}
	return "", fmt.Errorf("keyring resolver does not support credential ref %q", ref)
}

func secretFileCredentialName(credentialRef string) (string, error) {
	ref := strings.TrimSpace(credentialRef) // trimlowerlint:allow boundary canonicalization
	if ref == "secretfile" {
		return "", fmt.Errorf("secret file credential name must not be empty")
	}
	if strings.HasPrefix(strings.ToLower(ref), secretFileCredentialRefPrefix) { // trimlowerlint:allow boundary canonicalization
		name := strings.TrimSpace(ref[len(secretFileCredentialRefPrefix):]) // trimlowerlint:allow boundary canonicalization
		if name == "" {
			return "", fmt.Errorf("secret file credential name must not be empty")
		}
		return name, nil
	}
	return "", fmt.Errorf("file-backed secret resolver does not support credential ref %q", ref)
}
