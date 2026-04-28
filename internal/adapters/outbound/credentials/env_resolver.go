package credentials

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/metrofun/swobu/internal/domain/providercatalog"
)

const envCredentialRefPrefix = "env:"

// EnvResolver reads provider keys from the process environment.
//
// This adapter implements the "env" credential source that operators select
// in the cockpit credential choice row. The provider spec determines which
// environment variable to read.
type EnvResolver struct{}

// NewEnvResolver builds the env-based credential resolver.
func NewEnvResolver() EnvResolver {
	return EnvResolver{}
}

// ResolveCredential returns the provider token for one configured credential
// reference. When credentialRef is "env", it reads the provider's default env
// key; when credentialRef is "env:<KEY>", it reads the explicit key.
func (r EnvResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	_ = ctx
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
	ref := strings.TrimSpace(credentialRef)
	if ref == "" {
		return "", fmt.Errorf("credential ref must not be empty")
	}
	if ref == "env" {
		envKey := strings.TrimSpace(providercatalog.DefaultEnvKeyForSpec(providerSpec))
		if envKey == "" {
			return "", fmt.Errorf("provider %q has no default env key", providerSpec)
		}
		return envKey, nil
	}
	if !strings.HasPrefix(strings.ToLower(ref), envCredentialRefPrefix) {
		return "", fmt.Errorf("env resolver does not support credential ref %q", ref)
	}
	name := strings.TrimSpace(ref[len(envCredentialRefPrefix):])
	if name == "" {
		return "", fmt.Errorf("env credential name must not be empty")
	}
	return name, nil
}
