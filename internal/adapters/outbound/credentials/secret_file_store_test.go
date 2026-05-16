package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretFileStore_RoundTrip(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))
	store := secretFileStore{}
	if err := store.Store("chatgpt/default", "token-123"); err != nil {
		t.Fatalf("Store returned error: %v", err)
	}
	token, err := store.Resolve("chatgpt/default")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if token != "token-123" {
		t.Fatalf("token=%q want token-123", token)
	}
}

func TestSecretFileStore_IsProviderAgnostic(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))
	store := secretFileStore{}
	if err := store.Store("openai/default", "token-123"); err != nil {
		t.Fatalf("Store returned error: %v", err)
	}
	token, err := store.Resolve("openai/default")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if token != "token-123" {
		t.Fatalf("token=%q want token-123", token)
	}
}

func TestSecretFileStore_CorruptFileFailsFast(t *testing.T) {
	root := filepath.Join(t.TempDir(), "swobu-home")
	t.Setenv("SWOBU_HOME", root)
	path := filepath.Join(root, "state", "auth", "chatgpt.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	store := secretFileStore{}
	_, err := store.Resolve("chatgpt/default")
	if err == nil {
		t.Fatal("expected decode failure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "decode failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecretFileStore_WritesPrivatePermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "swobu-home")
	t.Setenv("SWOBU_HOME", root)
	path := filepath.Join(root, "state", "auth", "chatgpt.json")
	store := secretFileStore{}
	if err := store.Store("chatgpt/default", "token-123"); err != nil {
		t.Fatalf("Store returned error: %v", err)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat credential dir: %v", err)
	}
	if dirInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf("credential dir permissions = %o want no group/other bits", dirInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat credential file: %v", err)
	}
	if fileInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf("credential file permissions = %o want no group/other bits", fileInfo.Mode().Perm())
	}
}
