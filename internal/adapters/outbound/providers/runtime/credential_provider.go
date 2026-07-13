package runtime

import "context"

// CredentialProvider resolves credential references into provider tokens.
type CredentialProvider interface {
	ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error)
}
