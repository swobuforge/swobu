package credentials

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// CredentialResolver composes credential source adapters behind one provider-facing seam.
type CredentialResolver struct {
	byKind map[credentialref.Kind]CredentialSourceResolver
}

func NewResolver() CredentialResolver {
	return CredentialResolver{
		byKind: newSourceResolverRegistry(),
	}
}

func (r CredentialResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	ref := credentialref.Parse(credentialRef)
	slog.Debug("credential resolve requested",
		"component", "credentials",
		"provider_spec", strings.TrimSpace(strings.ToLower(providerSpec)), // trimlowerlint:allow boundary canonicalization
		"ref_kind", fmt.Sprintf("%v", ref.Kind()),
	)
	resolver, ok := r.byKind[ref.Kind()]
	if !ok {
		return "", fmt.Errorf("unsupported credential ref %q", ref.String())
	}
	return resolver.ResolveCredential(ctx, providerSpec, credentialRef)
}
