package credentials

import (
	"context"
	"fmt"
	"strings"
	"time"

	keyringcommodity "github.com/zalando/go-keyring"
)

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
	if strings.TrimSpace(scope) == "" || strings.TrimSpace(user) == "" { // swobu:io-string source=boundary
		return "", fmt.Errorf("keyring scope and user are required")
	}
	return keyringcommodity.Get(scope, user)
}

// StoredSecretResolver resolves secret references against the OS keyring only.
// Reference kind is the storage authority; fallback selection occurs only while
// creating a new materialized credential and returns the actual selected kind.
type StoredSecretResolver struct {
	client KeyringClient
}

func NewStoredSecretResolver(client KeyringClient) StoredSecretResolver {
	if client == nil {
		client = OSKeyringClient{}
	}
	return StoredSecretResolver{client: client}
}

// ResolveCredential returns the access token stored behind a keyring reference.
func (r StoredSecretResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	_ = ctx
	ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
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
		err = fmt.Errorf("keyring lookup timed out for %q", keyName)
	}
	if err != nil {
		return "", fmt.Errorf("keyring lookup failed for %q: %w", keyName, err)
	}
	accessToken, tokenErr := decodeStoredAccessToken(token, keyName, "keyring")
	if tokenErr != nil {
		return "", tokenErr
	}
	return accessToken, nil
}

func decodeStoredAccessToken(raw string, keyName string, source string) (string, error) {
	token := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if token == "" {
		return "", fmt.Errorf("%s token for %q is empty", source, keyName)
	}
	bundle, _, err := DecodeTokenBundle(token)
	if err != nil {
		return "", fmt.Errorf("%s token for %q is invalid: %w", source, keyName, err)
	}
	return bundle.AccessToken, nil
}

func KeyringScopeForProvider(providerSpec string) string {
	spec := strings.TrimSpace(strings.ToLower(providerSpec)) // swobu:io-string source=boundary
	if spec == "" {
		spec = "provider"
	}
	return keyringNamespace + "/" + spec
}

func CanonicalStoredSecretName(providerSpec string) string {
	spec := strings.TrimSpace(strings.ToLower(providerSpec)) // swobu:io-string source=boundary
	if spec == "" {
		return "default"
	}
	return spec + "/default"
}

func secretCredentialName(providerSpec, credentialRef string) (string, error) {
	ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if ref == "secret" {
		return CanonicalStoredSecretName(providerSpec), nil
	}
	if strings.HasPrefix(strings.ToLower(ref), secretCredentialRefPrefix) { // swobu:io-string source=boundary
		name := strings.TrimSpace(ref[len(secretCredentialRefPrefix):]) // swobu:io-string source=boundary
		if name == "" {
			return "", fmt.Errorf("secret credential name must not be empty")
		}
		return name, nil
	}
	return "", fmt.Errorf("keyring resolver does not support credential ref %q", ref)
}

func secretFileCredentialName(credentialRef string) (string, error) {
	ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if ref == "secretfile" {
		return "", fmt.Errorf("secret file credential name must not be empty")
	}
	if strings.HasPrefix(strings.ToLower(ref), secretFileCredentialRefPrefix) { // swobu:io-string source=boundary
		name := strings.TrimSpace(ref[len(secretFileCredentialRefPrefix):]) // swobu:io-string source=boundary
		if name == "" {
			return "", fmt.Errorf("secret file credential name must not be empty")
		}
		return name, nil
	}
	return "", fmt.Errorf("file-backed secret resolver does not support credential ref %q", ref)
}
