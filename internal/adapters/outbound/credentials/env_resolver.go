package credentials

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/profile"
)

const envCredentialRefPrefix = "env:"

// EnvResolver reads provider keys from the process environment.
//
// This adapter implements the "env" credential source that operators select
// in the local operator credential selection. The provider spec determines which
// environment variable to read.
type EnvCredentialSourceResolver struct{}

// NewEnvResolver builds the env-based credential resolver.
func NewEnvResolver() EnvCredentialSourceResolver {
	return EnvCredentialSourceResolver{}
}

// ResolveCredential returns the provider token for one configured credential
// reference. When credentialRef is "env", it reads the provider's default env
// key; when credentialRef is "env:<KEY>", it reads the explicit key.
func (r EnvCredentialSourceResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	scope, err := credentialref.NewProviderScope(providerSpec)
	if err != nil {
		return "", err
	}
	return r.ResolveScopedCredential(ctx, scope, credentialRef)
}

// ResolveScopedCredential handles environment references whose default key is
// defined by provider identity. Connection references must name their
// environment variable explicitly.
func (r EnvCredentialSourceResolver) ResolveScopedCredential(ctx context.Context, scope credentialref.Scope, credentialRef string) (string, error) {
	_ = ctx
	if !scope.Valid() {
		return "", fmt.Errorf("credential scope is invalid")
	}
	providerSpec := scope.Identifier()
	if scope.Kind() == credentialref.ScopeConnection && strings.EqualFold(strings.TrimSpace(credentialRef), "env") {
		return "", fmt.Errorf("Connection environment credentials require an explicit variable")
	}
	envKey, err := envCredentialName(providerSpec, credentialRef)
	if err != nil {
		return "", err
	}

	val := os.Getenv(envKey)
	if val == "" {
		return "", fmt.Errorf("env variable %q is not set", envKey)
	}
	return val, nil
}

func envCredentialName(providerSpec string, credentialRef string) (string, error) {
	ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if ref == "" {
		return "", fmt.Errorf("credential ref must not be empty")
	}
	if ref == "env" {
		envKey := strings.TrimSpace(profile.DefaultEnvKeyForSpec(providerSpec)) // swobu:io-string source=boundary
		if envKey == "" {
			return "", fmt.Errorf("provider %q has no default env key", providerSpec)
		}
		return envKey, nil
	}
	if !strings.HasPrefix(strings.ToLower(ref), envCredentialRefPrefix) { // swobu:io-string source=boundary
		return "", fmt.Errorf("env resolver does not support credential ref %q", ref)
	}
	name := strings.TrimSpace(ref[len(envCredentialRefPrefix):]) // swobu:io-string source=boundary
	if name == "" {
		return "", fmt.Errorf("env credential name must not be empty")
	}
	return name, nil
}
