package credentials

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// CredentialSourceResolver resolves one credential source reference into a token.
type CredentialSourceResolver interface {
	ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error)
}

var (
	_ CredentialSourceResolver = EnvResolver{}
	_ CredentialSourceResolver = FileResolver{}
	_ CredentialSourceResolver = KeyringResolver{}
)

// secretFileResolver adapts secretfile references onto secret-file storage.
type secretFileResolver struct {
	store *secretFileStore
}

func (r secretFileResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	_ = ctx
	_ = providerSpec
	keyName, err := secretFileCredentialName(credentialRef)
	if err != nil {
		return "", err
	}
	return r.store.Resolve(keyName)
}

var _ CredentialSourceResolver = secretFileResolver{}

func newSourceResolverRegistry() map[credentialref.Kind]CredentialSourceResolver {
	return map[credentialref.Kind]CredentialSourceResolver{
		credentialref.KindEnv:      NewEnvResolver(),
		credentialref.KindFile:     NewFileResolver(),
		credentialref.KindSecret:   NewKeyringResolver(nil),
		credentialref.KindKeychain: NewKeyringResolver(nil),
		credentialref.KindSecretFile: secretFileResolver{
			store: &secretFileStore{},
		},
	}
}
