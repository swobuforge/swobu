package credentials

import (
	"fmt"
	"strings"
	"testing"
)

func TestStoreStoredSecret_ValidatesInputs(t *testing.T) {
	err := StoreStoredSecret("", "openrouter/default", "token")
	if err == nil || !strings.Contains(err.Error(), "provider spec is required") {
		t.Fatalf("err = %v, want provider-spec validation", err)
	}

	err = StoreStoredSecret("openrouter", "", "token")
	if err == nil || !strings.Contains(err.Error(), "stored secret name is required") {
		t.Fatalf("err = %v, want key-name validation", err)
	}

	err = StoreStoredSecret("openrouter", "openrouter/default", "")
	if err == nil || !strings.Contains(err.Error(), "stored secret value is required") {
		t.Fatalf("err = %v, want key-value validation", err)
	}
}

func TestStoreStoredSecret_WritesProviderScopedScope(t *testing.T) {
	orig := keyringSet
	t.Cleanup(func() { keyringSet = orig })

	called := false
	keyringSet = func(scope, user, pass string) error {
		called = true
		if scope != KeyringScopeForProvider("openrouter") {
			t.Fatalf("scope = %q", scope)
		}
		if user != "openrouter/default" {
			t.Fatalf("user = %q", user)
		}
		bundle, _, err := DecodeTokenBundle(pass)
		if err != nil {
			t.Fatalf("decode stored bundle: %v", err)
		}
		if bundle.AccessToken != "token-123" {
			t.Fatalf("access_token=%q", bundle.AccessToken)
		}
		return nil
	}

	if err := StoreStoredSecret("openrouter", "openrouter/default", "token-123"); err != nil {
		t.Fatalf("StoreStoredSecret returned error: %v", err)
	}
	if !called {
		t.Fatal("expected keyring write to be called")
	}
}

func TestStoreStoredSecret_CustomUsesCanonicalProviderScope(t *testing.T) {
	orig := keyringSet
	t.Cleanup(func() { keyringSet = orig })
	keyringSet = func(scope, user, pass string) error {
		if scope != "swobu/custom" {
			t.Fatalf("scope = %q, want swobu/custom", scope)
		}
		return nil
	}
	if err := StoreStoredSecret("custom", "custom/default", "token-123"); err != nil {
		t.Fatal(err)
	}
}

func TestStoreStoredSecret_FallsBackToFileWhenKeyringUnavailable(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir()+"/swobu-home")
	orig := keyringSet
	t.Cleanup(func() { keyringSet = orig })
	origGet := keyringGet
	t.Cleanup(func() { keyringGet = origGet })

	keyringSet = func(scope, user, pass string) error {
		return fmt.Errorf("backend unavailable")
	}
	keyringGet = func(scope, user string) (string, error) {
		return "", fmt.Errorf("backend unavailable")
	}

	if err := StoreStoredSecret("openrouter", "openrouter/default", "token-123"); err != nil {
		t.Fatalf("StoreStoredSecret returned error: %v", err)
	}
	raw, err := ResolveStoredSecretByRef("openrouter", "secret:openrouter/default")
	if err != nil {
		t.Fatalf("ResolveStoredSecretByRef returned error: %v", err)
	}
	bundle, _, err := DecodeTokenBundle(raw)
	if err != nil {
		t.Fatalf("decode fallback bundle: %v", err)
	}
	if bundle.AccessToken != "token-123" {
		t.Fatalf("access_token=%q want token-123", bundle.AccessToken)
	}
}

func TestStoreMaterializedCredential_AutoFallsBackToFile(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir()+"/swobu-home")
	orig := keyringSet
	t.Cleanup(func() { keyringSet = orig })
	keyringSet = func(scope, user, pass string) error {
		return fmt.Errorf("backend unavailable")
	}

	ref, err := StoreMaterializedCredential("chatgpt", "chatgpt/default", "token-123", CredentialWritePolicyAuto)
	if err != nil {
		t.Fatalf("StoreMaterializedCredential returned error: %v", err)
	}
	if ref != "secretfile:chatgpt/default" {
		t.Fatalf("ref=%q", ref)
	}
}

func TestStoreMaterializedCredential_FileWritesWithoutKeyring(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir()+"/swobu-home")
	orig := keyringSet
	t.Cleanup(func() { keyringSet = orig })
	keyringSet = func(scope, user, pass string) error {
		t.Fatalf("unexpected keyring call scope=%q user=%q", scope, user)
		return nil
	}

	ref, err := StoreMaterializedCredential("chatgpt", "chatgpt/default", "token-123", CredentialWritePolicyFile)
	if err != nil {
		t.Fatalf("StoreMaterializedCredential returned error: %v", err)
	}
	if ref != "secretfile:chatgpt/default" {
		t.Fatalf("ref=%q", ref)
	}
}

func TestStoreSecretByRef_StoredSecretFallsBackToFileWhenKeyringUnavailable(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir()+"/swobu-home")
	origSet := keyringSet
	t.Cleanup(func() { keyringSet = origSet })
	origGet := keyringGet
	t.Cleanup(func() { keyringGet = origGet })

	keyringSet = func(scope, user, pass string) error {
		return fmt.Errorf("backend unavailable")
	}
	keyringGet = func(scope, user string) (string, error) {
		return "", fmt.Errorf("backend unavailable")
	}

	raw, err := EncodeTokenBundle(TokenBundle{AccessToken: "token-123"})
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}
	if err := StoreSecretByRef("openrouter", "secret:openrouter/default", raw); err != nil {
		t.Fatalf("StoreSecretByRef returned error: %v", err)
	}
	got, err := ResolveStoredSecretByRef("openrouter", "secret:openrouter/default")
	if err != nil {
		t.Fatalf("ResolveStoredSecretByRef returned error: %v", err)
	}
	if got != raw {
		t.Fatalf("resolved raw bundle mismatch: got %q want %q", got, raw)
	}
}
