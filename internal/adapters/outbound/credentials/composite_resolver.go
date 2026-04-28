package credentials

import (
	"context"
	"fmt"
	"strings"
)

// CredentialResolver composes credential source adapters behind one provider-facing seam.
type CredentialResolver struct {
	env     EnvResolver
	keyring KeyringResolver
	file    FileResolver
}

func NewResolver() CredentialResolver {
	return CredentialResolver{
		env:     NewEnvResolver(),
		keyring: NewKeyringResolver(nil),
		file:    NewFileResolver(),
	}
}

func (r CredentialResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	ref := strings.TrimSpace(strings.ToLower(credentialRef))
	switch {
	case ref == "env" || strings.HasPrefix(ref, envCredentialRefPrefix):
		return r.env.ResolveCredential(ctx, providerSpec, credentialRef)
	case ref == "keychain" || strings.HasPrefix(ref, keychainCredentialRefPrefix):
		return r.keyring.ResolveCredential(ctx, providerSpec, credentialRef)
	case ref == "file" || strings.HasPrefix(ref, fileCredentialRefPrefix) || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "~/"):
		return r.file.ResolveCredential(ctx, providerSpec, credentialRef)
	default:
		return "", fmt.Errorf("unsupported credential ref %q", strings.TrimSpace(credentialRef))
	}
}
