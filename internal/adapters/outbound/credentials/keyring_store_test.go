package credentials

import (
	"fmt"
	"strings"
	"testing"
)

func TestStoreMaterializedCredential_ValidatesInputs(t *testing.T) {
	_, err := StoreMaterializedCredential("", "openrouter/default", "token", CredentialWritePolicyAuto)
	if err == nil || !strings.Contains(err.Error(), "provider spec is required") {
		t.Fatalf("err = %v, want provider-spec validation", err)
	}

	_, err = StoreMaterializedCredential("openrouter", "", "token", CredentialWritePolicyAuto)
	if err == nil || !strings.Contains(err.Error(), "stored secret name is required") {
		t.Fatalf("err = %v, want key-name validation", err)
	}

	_, err = StoreMaterializedCredential("openrouter", "openrouter/default", "", CredentialWritePolicyAuto)
	if err == nil || !strings.Contains(err.Error(), "stored secret value is required") {
		t.Fatalf("err = %v, want key-value validation", err)
	}
}

func TestStoreMaterializedCredential_WritesProviderScopedScope(t *testing.T) {
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

	if _, err := StoreMaterializedCredential("openrouter", "openrouter/default", "token-123", CredentialWritePolicyAuto); err != nil {
		t.Fatalf("StoreMaterializedCredential returned error: %v", err)
	}
	if !called {
		t.Fatal("expected keyring write to be called")
	}
}

func TestStoreMaterializedCredential_CustomUsesCanonicalProviderScope(t *testing.T) {
	orig := keyringSet
	t.Cleanup(func() { keyringSet = orig })
	keyringSet = func(scope, user, pass string) error {
		if scope != "swobu/custom" {
			t.Fatalf("scope = %q, want swobu/custom", scope)
		}
		return nil
	}
	if _, err := StoreMaterializedCredential("custom", "custom/default", "token-123", CredentialWritePolicyAuto); err != nil {
		t.Fatal(err)
	}
}

func TestSecretReferenceDoesNotReadAutoFileFallback(t *testing.T) {
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

	ref, err := StoreMaterializedCredential("openrouter", "openrouter/default", "token-123", CredentialWritePolicyAuto)
	if err != nil {
		t.Fatalf("StoreMaterializedCredential returned error: %v", err)
	}
	if ref != "secretfile:openrouter/default" {
		t.Fatalf("ref = %q, want file authority", ref)
	}
	if _, err := ResolveStoredSecretByRef("openrouter", "secret:openrouter/default"); err == nil {
		t.Fatal("secret reference resolved through file fallback")
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

func TestStoreSecretByRef_KeyringFailureDoesNotWriteShadowFile(t *testing.T) {
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
	if err := StoreSecretByRef("openrouter", "secret:openrouter/default", raw); err == nil {
		t.Fatal("StoreSecretByRef succeeded through file fallback")
	}
	if _, err := (&secretFileStore{}).ResolveRaw("openrouter/default"); err == nil {
		t.Fatal("failed secret refresh wrote a shadow file")
	}
}
