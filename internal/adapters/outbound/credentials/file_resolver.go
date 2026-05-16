package credentials

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const fileCredentialRefPrefix = "file:"
const maxCredentialFileBytes = 16 * 1024

// FileResolver reads provider keys from a local credential file.
type FileResolver struct{}

// NewFileResolver builds the file-based credential resolver.
func NewFileResolver() FileResolver {
	return FileResolver{}
}

// ResolveCredential returns the provider token for one configured credential
// reference. Supported refs: "file:/abs/path", "file:~/path", or direct
// absolute/tilde paths.
func (r FileResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	_ = ctx
	_ = providerSpec
	path, err := fileCredentialPath(credentialRef)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("credential file %q could not be read", path)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("credential file %q must not be a symlink", path)
	}
	if info.Size() > maxCredentialFileBytes {
		return "", fmt.Errorf("credential file %q exceeds maximum allowed size", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("credential file %q could not be read", path)
	}
	token := strings.TrimSpace(string(raw)) // trimlowerlint:allow boundary canonicalization
	if token == "" {
		return "", fmt.Errorf("credential file %q is empty", path)
	}
	return token, nil
}

func fileCredentialPath(credentialRef string) (string, error) {
	ref := strings.TrimSpace(credentialRef) // trimlowerlint:allow boundary canonicalization
	if ref == "" {
		return "", fmt.Errorf("credential ref must not be empty")
	}
	path := ref
	if strings.EqualFold(ref, "file") || strings.EqualFold(ref, "file:") {
		return "", fmt.Errorf("credential file path must not be empty")
	}
	if strings.HasPrefix(strings.ToLower(ref), fileCredentialRefPrefix) { // trimlowerlint:allow boundary canonicalization
		path = strings.TrimSpace(ref[len(fileCredentialRefPrefix):]) // trimlowerlint:allow boundary canonicalization
		if path == "" {
			return "", fmt.Errorf("credential file path must not be empty")
		}
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" { // trimlowerlint:allow boundary canonicalization
			return "", fmt.Errorf("home directory is unavailable for credential file")
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("credential file path must be absolute")
	}
	return filepath.Clean(path), nil
}
