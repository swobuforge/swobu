package credentials

import (
	"fmt"
	"strings"
	"testing"
)

func TestStoreKeychainCredential_ValidatesInputs(t *testing.T) {
	t.Parallel()

	err := StoreKeychainCredential("", "openrouter/default", "token")
	if err == nil || !strings.Contains(err.Error(), "provider spec is required") {
		t.Fatalf("err = %v, want provider-spec validation", err)
	}

	err = StoreKeychainCredential("openrouter", "", "token")
	if err == nil || !strings.Contains(err.Error(), "keychain key name is required") {
		t.Fatalf("err = %v, want key-name validation", err)
	}

	err = StoreKeychainCredential("openrouter", "openrouter/default", "")
	if err == nil || !strings.Contains(err.Error(), "keychain key value is required") {
		t.Fatalf("err = %v, want key-value validation", err)
	}
}

func TestStoreKeychainCredential_WritesProviderScopedScope(t *testing.T) {
	t.Parallel()

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
		if pass != "token-123" {
			t.Fatalf("pass = %q", pass)
		}
		return nil
	}

	if err := StoreKeychainCredential("openrouter", "openrouter/default", "token-123"); err != nil {
		t.Fatalf("StoreKeychainCredential returned error: %v", err)
	}
	if !called {
		t.Fatal("expected keyring write to be called")
	}
}

func TestStoreKeychainCredential_FallsBackToMemoryWhenKeyringUnavailable(t *testing.T) {
	t.Parallel()

	orig := keyringSet
	t.Cleanup(func() { keyringSet = orig })

	keyringSet = func(scope, user, pass string) error {
		return fmt.Errorf("backend unavailable")
	}

	if err := StoreKeychainCredential("openrouter", "openrouter/default", "token-123"); err != nil {
		t.Fatalf("StoreKeychainCredential returned error: %v", err)
	}
	token, ok := getMemoryFallbackSecret("openrouter", "openrouter/default")
	if !ok {
		t.Fatal("expected in-memory fallback secret to be set")
	}
	if token != "token-123" {
		t.Fatalf("fallback token = %q", token)
	}
}
