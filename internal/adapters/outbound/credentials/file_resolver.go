package credentials

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

const fileCredentialRefPrefix = "file:"
const maxCredentialFileBytes = 16 * 1024

// FileResolver reads provider keys from a local credential file.
type FileCredentialSourceResolver struct{}

// NewFileResolver builds the file-based credential resolver.
func NewFileResolver() FileCredentialSourceResolver {
	return FileCredentialSourceResolver{}
}

// ResolveCredential returns the provider token for one configured credential
// reference. Supported refs: "file:/abs/path", "file:~/path", or direct
// absolute/tilde paths.
func (r FileCredentialSourceResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	scope, err := credentialref.NewProviderScope(providerSpec)
	if err != nil {
		return "", err
	}
	return r.ResolveScopedCredential(ctx, scope, credentialRef)
}

func (r FileCredentialSourceResolver) ResolveScopedCredential(ctx context.Context, scope credentialref.Scope, credentialRef string) (string, error) {
	_ = ctx
	if !scope.Valid() {
		return "", fmt.Errorf("credential scope is invalid")
	}
	path, err := fileCredentialPath(credentialRef)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("credential file %q could not be read", path)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("credential file %q must resolve to a regular file", path)
	}
	if info.Size() > maxCredentialFileBytes {
		return "", fmt.Errorf("credential file %q exceeds maximum allowed size", path)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxCredentialFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("credential file %q could not be read", path)
	}
	if len(raw) > maxCredentialFileBytes {
		return "", fmt.Errorf("credential file %q exceeds maximum allowed size", path)
	}
	token := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if token == "" {
		return "", fmt.Errorf("credential file %q is empty", path)
	}
	return token, nil
}

func fileCredentialPath(credentialRef string) (string, error) {
	ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if ref == "" {
		return "", fmt.Errorf("credential ref must not be empty")
	}
	path := ref
	if strings.EqualFold(ref, "file") || strings.EqualFold(ref, "file:") {
		return "", fmt.Errorf("credential file path must not be empty")
	}
	if strings.HasPrefix(strings.ToLower(ref), fileCredentialRefPrefix) { // swobu:io-string source=boundary
		path = strings.TrimSpace(ref[len(fileCredentialRefPrefix):]) // swobu:io-string source=boundary
		if path == "" {
			return "", fmt.Errorf("credential file path must not be empty")
		}
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" { // swobu:io-string source=boundary
			return "", fmt.Errorf("home directory is unavailable for credential file")
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("credential file path must be absolute")
	}
	return filepath.Clean(path), nil
}
