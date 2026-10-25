package runtime

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/provider"
)

// CredentialProvider resolves credential references into provider tokens.
type CredentialProvider interface {
	ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error)
}

// ResolveTargetCredential preserves the exact owner captured for an attempt.
// A Connection-bound target never falls back to provider-owned material.
func ResolveTargetCredential(ctx context.Context, resolver CredentialProvider, target provider.TargetSnapshot) (string, error) {
	if resolver == nil {
		return "", errors.New("credential resolver is required")
	}
	if _, _, bound := target.ConnectionIdentity(); bound {
		scoped, ok := resolver.(interface {
			ResolveScopedCredential(context.Context, credentialref.Scope, string) (string, error)
		})
		if !ok {
			return "", errors.New("Connection credential resolver is required")
		}
		scope, err := target.ConnectionCredentialScope()
		if err != nil {
			return "", err
		}
		return scoped.ResolveScopedCredential(ctx, scope, target.CredentialRef)
	}
	// TODO: Remove provider-scoped execution when targets complete reference cutover.
	return resolver.ResolveCredential(ctx, target.ProviderID(), target.CredentialRef)
}
