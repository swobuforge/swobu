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

func TestStoreKeychainCredential_NormalizesBackendError(t *testing.T) {
	t.Parallel()

	orig := keyringSet
	t.Cleanup(func() { keyringSet = orig })

	keyringSet = func(scope, user, pass string) error {
		return fmt.Errorf("backend unavailable")
	}

	err := StoreKeychainCredential("openrouter", "openrouter/default", "token-123")
	if err == nil {
		t.Fatal("err = nil, want failure")
	}
	if !strings.Contains(err.Error(), "keyring write failed for \"openrouter/default\"") {
		t.Fatalf("err = %v, want normalized failure", err)
	}
}
