package credentials

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
	keyringcommodity "github.com/zalando/go-keyring"
)

func TestConnectionCredentialFileSlotsIsolationAndRetirement(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "home"))
	first, err := StoreConnectionCredential("cn_a", "fixture-first", CredentialWritePolicyFile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := StoreConnectionCredential("cn_a", "fixture-second", CredentialWritePolicyFile)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("staging overwrote old slot")
	}
	scope, _ := credentialref.NewConnectionScope("cn_a")
	other, _ := credentialref.NewConnectionScope("cn_b")
	provider, _ := credentialref.NewProviderScope("openai")
	resolver := secretFileCredentialSourceResolver{store: &secretFileStore{}}
	for _, test := range []struct {
		scope     credentialref.Scope
		ref, want string
	}{{scope, first, "fixture-first"}, {scope, second, "fixture-second"}, {other, first, ""}, {provider, first, ""}} {
		got, err := resolver.ResolveScopedCredential(context.Background(), test.scope, test.ref)
		if test.want == "" {
			if err == nil {
				t.Fatal("foreign owner resolved Connection slot")
			}
		} else if err != nil || got != test.want {
			t.Fatal("Connection slot did not roundtrip")
		}
	}
	if err := RetireConnectionCredential("cn_b", first); err == nil {
		t.Fatal("foreign owner retired slot")
	}
	if err := RetireConnectionCredential("cn_a", "env:EXTERNAL"); err == nil {
		t.Fatal("retired external locator")
	}
	if err := RetireConnectionCredential("cn_a", first); err != nil {
		t.Fatal(err)
	}
	if err := RetireConnectionCredential("cn_a", first); err != nil {
		t.Fatal("retirement not idempotent")
	}
	if _, err := resolver.ResolveScopedCredential(context.Background(), scope, first); err == nil {
		t.Fatal("retired material remained")
	}
	if got, err := resolver.ResolveScopedCredential(context.Background(), scope, second); err != nil || got != "fixture-second" {
		t.Fatal("retirement removed unrelated slot")
	}
}

func TestConnectionCredentialKeyringSlotsAndBackendAuthority(t *testing.T) {
	originalSet, originalDelete, originalGet := keyringSet, keyringDelete, keyringGet
	t.Cleanup(func() { keyringSet, keyringDelete, keyringGet = originalSet, originalDelete, originalGet })
	values := map[string]string{}
	keyringGet = func(scope, user string) (string, error) {
		value, found := values[scope+"|"+user]
		if !found {
			return "", keyringcommodity.ErrNotFound
		}
		return value, nil
	}
	keyringSet = func(scope, user, secret string) error { values[scope+"|"+user] = secret; return nil }
	keyringDelete = func(scope, user string) error {
		key := scope + "|" + user
		if _, exists := values[key]; !exists {
			return keyringcommodity.ErrNotFound
		}
		delete(values, key)
		return nil
	}
	first, err := StoreConnectionCredential("cn_a", "fixture-first", CredentialWritePolicyKeyring)
	if err != nil {
		t.Fatal(err)
	}
	second, err := StoreConnectionCredential("cn_a", "fixture-second", CredentialWritePolicyKeyring)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := credentialref.NewConnectionScope("cn_a")
	firstName, err := scopedSecretCredentialName(scope, first)
	if err != nil {
		t.Fatal(err)
	}
	secondName, err := scopedSecretCredentialName(scope, second)
	if err != nil || firstName == secondName {
		t.Fatal("keyring slot reused")
	}
	if len(values) != 2 || values[KeyringScopeForCredential(scope)+"|"+firstName] == "" {
		t.Fatal("wrong keyring authority")
	}
	other, _ := credentialref.NewConnectionScope("cn_b")
	if _, err := scopedSecretCredentialName(other, first); err == nil {
		t.Fatal("foreign scope accepted")
	}
	provider, _ := credentialref.NewProviderScope("openai")
	if _, err := scopedSecretCredentialName(provider, first); err == nil {
		t.Fatal("provider accepted Connection slot")
	}
	if err := RetireConnectionCredential("cn_a", first); err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[KeyringScopeForCredential(scope)+"|"+secondName] == "" {
		t.Fatal("wrong keyring slot retired")
	}
	keyringDelete = func(string, string) error { return errors.New("backend unavailable fixture-secret") }
	if err := RetireConnectionCredential("cn_a", second); err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatal("retirement error disclosed backend detail")
	}
}

func TestIndependentCredentialFileAdaptersDoNotLoseConcurrentSlots(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "home"))
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := (&secretFileStore{}).Store(fmt.Sprintf("fixture-%d", i), "fixture-value"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for i := range 20 {
		if got, err := (&secretFileStore{}).ResolveRaw(fmt.Sprintf("fixture-%d", i)); err != nil || got != "fixture-value" {
			t.Fatal("concurrent adapter lost slot")
		}
	}
}

func TestConnectionSlotGrammarAndProviderRefreshIsolation(t *testing.T) {
	for _, id := range []string{"", "Upper", "id/path", "id space"} {
		if _, err := credentialref.NewConnectionScope(id); err == nil {
			t.Fatal("invalid Connection scope")
		}
	}
	scope, _ := credentialref.NewConnectionScope("cn_a")
	for _, slot := range []string{"connections/cn_a/cred_", "connections/cn_a/cred_ABCDEF", "connections/cn_b/cred_" + strings.Repeat("a", 32), "connections/cn_a/cred_" + strings.Repeat("a", 32) + "/extra", "openai/default"} {
		if validConnectionSlot(scope, slot) {
			t.Fatal("invalid slot admitted")
		}
	}
	slot := "connections/cn_a/cred_" + strings.Repeat("a", 32)
	if _, err := StoreMaterializedCredential("openai", slot, "fixture", CredentialWritePolicyFile); err == nil {
		t.Fatal("provider writer can overwrite Connection slot")
	}
	for _, prefix := range []string{"secret:", "secretfile:"} {
		if _, _, err := parseStoredSecretRef("openai", prefix+slot); err == nil {
			t.Fatal("provider refresh can overwrite Connection slot")
		}
	}
}

func TestRegisteredConnectionSlotRetriesCannotOverwriteMaterial(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "home"))
	slot := "connections/cn_a/cred_" + strings.Repeat("c", 32)
	first, err := StoreConnectionCredentialSlot("cn_a", slot, "fixture-original", CredentialWritePolicyFile)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := StoreConnectionCredentialSlot("cn_a", slot, "fixture-original", CredentialWritePolicyFile)
	if err != nil || replay != first {
		t.Fatal("same-material retry failed")
	}
	_, err = StoreConnectionCredentialSlot("cn_a", slot, "fixture-changed", CredentialWritePolicyFile)
	if err == nil {
		t.Fatal("retry overwrote immutable staged material")
	}
	scope, _ := credentialref.NewConnectionScope("cn_a")
	got, err := NewResolver().ResolveScopedCredential(t.Context(), scope, first)
	if err != nil || got != "fixture-original" {
		t.Fatal("changed retry damaged original material")
	}
}

func TestConnectionSlotConcurrentWritersHaveOneImmutableWinner(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "home"))
	slot := "connections/cn_a/cred_" + strings.Repeat("d", 32)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for index := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := StoreConnectionCredentialSlot("cn_a", slot, fmt.Sprintf("synthetic-%d", index), CredentialWritePolicyFile)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrConnectionMaterialConflict) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful conflicting writers=%d", successes)
	}
}

func TestConnectionSlotKeyringReplayAndUnavailableRead(t *testing.T) {
	originalGet, originalSet := keyringGet, keyringSet
	defer func() { keyringGet, keyringSet = originalGet, originalSet }()
	slot := "connections/cn_a/cred_" + strings.Repeat("e", 32)
	material := ""
	writes := 0
	keyringGet = func(string, string) (string, error) {
		if material == "" {
			return "", keyringcommodity.ErrNotFound
		}
		return material, nil
	}
	keyringSet = func(_, _, value string) error { writes++; material = value; return nil }
	first, err := StoreConnectionCredentialSlot("cn_a", slot, "fixture-token", CredentialWritePolicyKeyring)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := StoreConnectionCredentialSlot("cn_a", slot, "fixture-token", CredentialWritePolicyKeyring)
	if err != nil || replay != first || writes != 1 {
		t.Fatal("retry wrote keyring again")
	}
	if _, err := StoreConnectionCredentialSlot("cn_a", slot, "fixture-other", CredentialWritePolicyKeyring); !errors.Is(err, ErrConnectionMaterialConflict) || writes != 1 {
		t.Fatal("changed retry wrote keyring")
	}
	keyringGet = func(string, string) (string, error) { return "", errors.New("fixture-private-backend") }
	_, err = StoreConnectionCredentialSlot("cn_a", slot, "fixture-token", CredentialWritePolicyKeyring)
	if err == nil || strings.Contains(err.Error(), "fixture-private") || writes != 1 {
		t.Fatal("unavailable lookup overwrote slot or leaked detail")
	}
}

func TestFreshConnectionRejectsAmbiguousWritePolicy(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "home"))
	originalGet, originalSet := keyringGet, keyringSet
	defer func() { keyringGet, keyringSet = originalGet, originalSet }()
	writes := 0
	keyringGet = func(string, string) (string, error) { return "", errors.New("synthetic-unavailable") }
	keyringSet = func(string, string, string) error { writes++; return errors.New("synthetic-unavailable") }
	if _, err := StoreConnectionCredential("cn_a", "fixture-fallback", CredentialWritePolicy("auto")); err == nil || writes != 0 {
		t.Fatal("ambiguous policy was accepted")
	}
}

func TestConnectionRetryComparesRefreshMaterialAndProtectsMalformedStorage(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "home"))
	slot := "connections/cn_a/cred_" + strings.Repeat("a", 32)
	first := `{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","issued_at":"2026-01-01T00:00:00Z"}`
	if _, err := StoreConnectionCredentialSlot("cn_a", slot, first, CredentialWritePolicyFile); err != nil {
		t.Fatal(err)
	}
	same := `{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","issued_at":"2026-02-01T00:00:00Z"}`
	if _, err := StoreConnectionCredentialSlot("cn_a", slot, same, CredentialWritePolicyFile); err != nil {
		t.Fatal("issuance metadata prevented same-material recovery")
	}
	changed := `{"access_token":"synthetic-access","refresh_token":"synthetic-other"}`
	if _, err := StoreConnectionCredentialSlot("cn_a", slot, changed, CredentialWritePolicyFile); !errors.Is(err, ErrConnectionMaterialConflict) {
		t.Fatal("different refresh token accepted")
	}
	// Existing malformed slot data is uncertainty, never proof that writing is safe.
	if err := (&secretFileStore{}).Store(slot, "{synthetic-malformed"); err != nil {
		t.Fatal(err)
	}
	if _, err := StoreConnectionCredentialSlot("cn_a", slot, "fixture-token", CredentialWritePolicyFile); !errors.Is(err, ErrConnectionMaterialConflict) {
		t.Fatal("malformed material overwritten")
	}
	stored, err := (&secretFileStore{}).ResolveRaw(slot)
	if err != nil || stored != "{synthetic-malformed" {
		t.Fatal("malformed slot changed")
	}
}

func TestConnectionStoredBundlePreservesMetadataAndExactOwnership(t *testing.T) {
	originalGet := keyringGet
	keyringGet = func(scope, user string) (string, error) { return "", keyringcommodity.ErrNotFound }
	t.Cleanup(func() { keyringGet = originalGet })
	t.Setenv("SWOBU_HOME", t.TempDir())
	raw := mustBundle(t, "fixture-owned")
	ref, err := StoreConnectionCredential("cn_bundle", raw, CredentialWritePolicyFile)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveConnectionStoredBundle("cn_bundle", ref)
	if err != nil || got != raw {
		t.Fatal("bundle did not roundtrip")
	}
	for _, test := range []struct{ id, ref string }{
		{"cn_other", ref}, {"cn_bundle", "env:FIXTURE"},
		{"cn_bundle", "secretfile:chatgpt/default"},
		{"cn_bundle", "secret:" + strings.TrimPrefix(ref, "secretfile:")},
	} {
		if _, err := ResolveConnectionStoredBundle(test.id, test.ref); err == nil {
			t.Fatal("bundle escaped owner or backend authority")
		}
	}
}
