package credentials

import (
	"context"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// CredentialSourceResolver resolves a reference under one explicit owner scope.
// Provider and Connection identity selects storage authority independently
// of the reference backend.
type CredentialSourceResolver interface {
	ResolveScopedCredential(ctx context.Context, scope credentialref.Scope, credentialRef string) (string, error)
}

var (
	_ CredentialSourceResolver = EnvCredentialSourceResolver{}
	_ CredentialSourceResolver = FileCredentialSourceResolver{}
	_ CredentialSourceResolver = StoredSecretResolver{}
)

// secretFileCredentialSourceResolver adapts secretfile references onto secret-file storage.
type secretFileCredentialSourceResolver struct {
	store *secretFileStore
}

func (r secretFileCredentialSourceResolver) ResolveScopedCredential(ctx context.Context, scope credentialref.Scope, credentialRef string) (string, error) {
	_ = ctx
	if !scope.Valid() {
		return "", fmt.Errorf("credential scope is invalid")
	}
	keyName, err := secretFileCredentialName(credentialRef)
	if err != nil {
		return "", err
	}
	if scope.Kind() != credentialref.ScopeConnection && strings.HasPrefix(keyName, "connections/") {
		return "", fmt.Errorf("Connection credentials require Connection scope")
	}
	if scope.Kind() == credentialref.ScopeConnection && !validConnectionSlot(scope, keyName) {
		return "", fmt.Errorf("credential is outside Connection scope")
	}
	return r.store.Resolve(keyName)
}

var _ CredentialSourceResolver = secretFileCredentialSourceResolver{}

func newSourceResolverRegistry() map[credentialref.Kind]CredentialSourceResolver {
	return map[credentialref.Kind]CredentialSourceResolver{
		credentialref.KindEnv:    NewEnvResolver(),
		credentialref.KindFile:   NewFileResolver(),
		credentialref.KindSecret: NewStoredSecretResolver(nil),
		credentialref.KindSecretFile: secretFileCredentialSourceResolver{
			store: &secretFileStore{},
		},
	}
}
