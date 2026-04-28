package credentials

import (
	"context"
	"fmt"
	"os"
	"testing"
)

type fakeKeyringClient struct {
	values map[string]string
	err    error
}

func (f fakeKeyringClient) Get(scope, user string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.values[scope+"|"+user], nil
}

func TestKeyringResolver_ResolveCredential_ExplicitKeyName(t *testing.T) {
	client := fakeKeyringClient{
		values: map[string]string{
			KeyringScopeForProvider("openrouter") + "|openrouter/default": "token-1",
		},
	}
	resolver := NewKeyringResolver(client)
	token, err := resolver.ResolveCredential(context.Background(), "openrouter", "keychain:openrouter/default")
	if err != nil {
		t.Fatalf("ResolveCredential returned error: %v", err)
	}
	if token != "token-1" {
		t.Fatalf("token = %q, want token-1", token)
	}
}

func TestKeyringResolver_ResolveCredential_BareKeychainUsesDefaultName(t *testing.T) {
	client := fakeKeyringClient{
		values: map[string]string{
			KeyringScopeForProvider("openai") + "|openai/default": "token-2",
		},
	}
	resolver := NewKeyringResolver(client)
	token, err := resolver.ResolveCredential(context.Background(), "openai", "keychain")
	if err != nil {
		t.Fatalf("ResolveCredential returned error: %v", err)
	}
	if token != "token-2" {
		t.Fatalf("token = %q, want token-2", token)
	}
}

func TestKeyringResolver_ResolveCredential_LookupFailure(t *testing.T) {
	resolver := NewKeyringResolver(fakeKeyringClient{err: fmt.Errorf("backend unavailable")})
	_, err := resolver.ResolveCredential(context.Background(), "openai", "keychain:openai/default")
	if err == nil {
		t.Fatalf("ResolveCredential returned nil error, want failure")
	}
}

func TestResolver_ResolveCredential_UnsupportedRef(t *testing.T) {
	resolver := NewResolver()
	_, err := resolver.ResolveCredential(context.Background(), "openai", "vault:/tmp/token")
	if err == nil {
		t.Fatalf("ResolveCredential returned nil error, want unsupported-ref failure")
	}
}

func TestEnvResolver_ResolveCredential_DefaultProviderKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "token-default")
	resolver := NewEnvResolver()
	token, err := resolver.ResolveCredential(context.Background(), "openrouter", "env")
	if err != nil {
		t.Fatalf("ResolveCredential returned error: %v", err)
	}
	if token != "token-default" {
		t.Fatalf("token = %q, want token-default", token)
	}
}

func TestEnvResolver_ResolveCredential_ExplicitKeyOverride(t *testing.T) {
	t.Setenv("LAB_API_KEY", "token-lab")
	resolver := NewEnvResolver()
	token, err := resolver.ResolveCredential(context.Background(), "openrouter", "env:LAB_API_KEY")
	if err != nil {
		t.Fatalf("ResolveCredential returned error: %v", err)
	}
	if token != "token-lab" {
		t.Fatalf("token = %q, want token-lab", token)
	}
}

func TestEnvResolver_ResolveCredential_ExplicitKeyEmpty(t *testing.T) {
	resolver := NewEnvResolver()
	_, err := resolver.ResolveCredential(context.Background(), "openrouter", "env:")
	if err == nil {
		t.Fatalf("ResolveCredential returned nil error, want parse failure")
	}
}

func TestFileResolver_ResolveCredential_FilePrefixPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/token"
	if err := os.WriteFile(path, []byte(" token-file \n"), 0o600); err != nil {
		t.Fatalf("write file token: %v", err)
	}
	resolver := NewFileResolver()
	token, err := resolver.ResolveCredential(context.Background(), "openrouter", "file:"+path)
	if err != nil {
		t.Fatalf("ResolveCredential returned error: %v", err)
	}
	if token != "token-file" {
		t.Fatalf("token = %q, want token-file", token)
	}
}

func TestFileResolver_ResolveCredential_HomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := home + "/.config/swobu/openrouter.key"
	if err := os.MkdirAll(home+"/.config/swobu", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("token-home"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	resolver := NewFileResolver()
	token, err := resolver.ResolveCredential(context.Background(), "openrouter", "file:~/.config/swobu/openrouter.key")
	if err != nil {
		t.Fatalf("ResolveCredential returned error: %v", err)
	}
	if token != "token-home" {
		t.Fatalf("token = %q, want token-home", token)
	}
}

func TestFileResolver_ResolveCredential_RejectsRelativePath(t *testing.T) {
	resolver := NewFileResolver()
	_, err := resolver.ResolveCredential(context.Background(), "openrouter", "file:token.txt")
	if err == nil {
		t.Fatalf("ResolveCredential returned nil error, want path failure")
	}
}
