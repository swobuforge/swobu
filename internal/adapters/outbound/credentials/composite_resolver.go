package credentials

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// CredentialSourceResolverRegistry composes credential source adapters behind
// one owner-scoped resolution seam.
type CredentialSourceResolverRegistry struct {
	byKind map[credentialref.Kind]CredentialSourceResolver
}

func NewResolver() CredentialSourceResolverRegistry {
	return CredentialSourceResolverRegistry{
		byKind: newSourceResolverRegistry(),
	}
}

// ResolveCredential preserves the existing provider credential contract.
func (r CredentialSourceResolverRegistry) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	scope, err := credentialref.NewProviderScope(providerSpec)
	if err != nil {
		return "", err
	}
	return r.ResolveScopedCredential(ctx, scope, credentialRef)
}

// ResolveScopedCredential resolves one reference against an explicit provider
// or MCP-source owner. Storage kind remains selected by the reference prefix.
func (r CredentialSourceResolverRegistry) ResolveScopedCredential(ctx context.Context, scope credentialref.Scope, credentialRef string) (string, error) {
	if !scope.Valid() {
		return "", fmt.Errorf("credential scope is invalid")
	}
	ref := credentialref.Parse(credentialRef)
	slog.Debug("credential resolve requested",
		"component", "credentials",
		"scope_kind", string(scope.Kind()),
		"scope_id", strings.TrimSpace(strings.ToLower(scope.Identifier())), // swobu:io-string source=boundary
		"ref_kind", fmt.Sprintf("%v", ref.Kind()),
	)
	resolver, ok := r.byKind[ref.Kind()]
	if !ok {
		return "", fmt.Errorf("unsupported credential ref %q", ref.String())
	}
	return resolver.ResolveScopedCredential(ctx, scope, credentialRef)
}
