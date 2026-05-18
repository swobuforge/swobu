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
	_ CredentialSourceResolver = EnvCredentialSourceResolver{}
	_ CredentialSourceResolver = FileCredentialSourceResolver{}
	_ CredentialSourceResolver = KeyringCredentialSourceResolver{}
)

// secretFileCredentialSourceResolver adapts secretfile references onto secret-file storage.
type secretFileCredentialSourceResolver struct {
	store *secretFileStore
}

func (r secretFileCredentialSourceResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	_ = ctx
	_ = providerSpec
	keyName, err := secretFileCredentialName(credentialRef)
	if err != nil {
		return "", err
	}
	return r.store.Resolve(keyName)
}

var _ CredentialSourceResolver = secretFileCredentialSourceResolver{}

func newSourceResolverRegistry() map[credentialref.Kind]CredentialSourceResolver {
	return map[credentialref.Kind]CredentialSourceResolver{
		credentialref.KindEnv:      NewEnvResolver(),
		credentialref.KindFile:     NewFileResolver(),
		credentialref.KindSecret:   NewKeyringResolver(nil),
		credentialref.KindKeychain: NewKeyringResolver(nil),
		credentialref.KindSecretFile: secretFileCredentialSourceResolver{
			store: &secretFileStore{},
		},
	}
}
