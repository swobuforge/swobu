package chatgpt

import (
	"context"
	"errors"
	"strings"

	outboundcredentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/provider"
)

// RefreshConnectionBundle performs only the provider token exchange for one
// captured Connection owner. Returned material is private execution input, never
// evidence or a public response. It neither writes the immutable source slot nor
// retries a provider request. The attempt owner must stage a new slot, compare
// the captured revision and publish its replacement before freezing retry facts.
func (e BackendAdapter) RefreshConnectionBundle(ctx context.Context, target provider.TargetSnapshot) (string, error) {
	invalid := errors.New("Connection credential refresh unavailable")
	if err := ctx.Err(); err != nil {
		return "", err
	}
	scope, err := target.ConnectionCredentialScope()
	if err != nil || target.ProviderID() != "chatgpt" || e.client == nil {
		return "", invalid
	}
	if _, ok := e.credentials.(outboundcredentials.CredentialSourceResolverRegistry); !ok {
		return "", invalid
	}
	kind := credentialref.Parse(target.CredentialRef).Kind()
	if kind != credentialref.KindSecret && kind != credentialref.KindSecretFile {
		return "", invalid
	}
	raw, err := outboundcredentials.ResolveConnectionStoredBundle(scope.Identifier(), target.CredentialRef)
	if err != nil {
		return "", invalid
	}
	bundle, isBundle, err := outboundcredentials.DecodeTokenBundle(raw)
	if err != nil || !isBundle || strings.TrimSpace(bundle.RefreshToken) == "" {
		return "", invalid
	}
	next, err := requestChatGPTTokenRefresh(ctx, e.client, bundle.RefreshToken)
	if err != nil {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", invalid
	}
	if next.RefreshToken == "" {
		next.RefreshToken = bundle.RefreshToken
	}
	priorAccount, priorFedRAMP := parseChatGPTRoutingClaims(bundle.IDToken)
	nextAccount, nextFedRAMP := parseChatGPTRoutingClaims(next.IDToken)
	// An explicit routing claim is authoritative for the refreshed material.
	// Reject an account or regulated-endpoint switch before publication; only
	// absent routing claims may inherit the captured bundle's routing token.
	if priorAccount != "" && nextAccount != "" && (priorAccount != nextAccount || priorFedRAMP != nextFedRAMP) {
		return "", invalid
	}
	if next.IDToken == "" || (priorAccount != "" && nextAccount == "") {
		next.IDToken = bundle.IDToken
	}
	encoded, err := outboundcredentials.EncodeTokenBundle(next)
	if err != nil {
		return "", invalid
	}
	return encoded, nil
}
