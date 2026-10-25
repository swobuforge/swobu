package connections

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	credentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/configstore"
	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

func TestStagingReplacementAndRecoveryThroughFileAdapter(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SWOBU_HOME", filepath.Join(dir, "home"))
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	registry, err := connectionfixture.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := configstore.OpenOrCreate(filepath.Join(dir, "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer workspaces.Close()
	lifecycle, err := domain.NewLifecycle(registry, workspaces)
	if err != nil {
		t.Fatal(err)
	}
	commands, err := NewCommands(registry, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := domain.ParseEndpoint("https://example.com/mcp")
	old := credentialref.Parse("env:OLD")
	spec, _ := domain.NewMCPHTTPSpec(endpoint, old)
	admit := func(context.Context) error { return nil }
	created, _, err := commands.Create(t.Context(), "Shared", spec, "local", "create", admit)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.Parse(created.ID)
	revision, _ := domain.ParseRevision(created.Revision)
	ref, err := commands.StageCredential(t.Context(), id, "synthetic-new-secret", admit, func(_ context.Context, id connectionidentity.ID, slot, secret string) (credentialref.Ref, error) {
		raw, err := credentials.StoreConnectionCredentialSlot(id.String(), slot, secret, credentials.CredentialWritePolicyFile)
		return credentialref.Parse(raw), err
	})
	if err != nil {
		t.Fatal(err)
	}
	replaced, _, err := commands.ReplaceCredentialBinding(t.Context(), id, revision, "", old, ref, true, "local", "replace", admit)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := connectionfixture.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.StagedCredentials()) != 2 {
		t.Fatal("restart lost staged backend candidates")
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "synthetic-new-secret") {
		t.Fatal("registry retained secret material")
	}
	scope, _ := credentialref.NewConnectionScope(id.String())
	resolver := credentials.NewResolver()
	got, err := resolver.ResolveScopedCredential(t.Context(), scope, ref.String())
	if err != nil || got != "synthetic-new-secret" {
		t.Fatal("committed material cannot resolve")
	}
	cleanup := func(_ context.Context, id connectionidentity.ID, ref credentialref.Ref) error {
		// The file policy never wrote the alternate keyring candidate. Avoid an OS
		// keyring call in this proof; real missing-keyring behavior has adapter proof.
		if ref.Kind() == credentialref.KindSecret {
			return nil
		}
		return credentials.RetireConnectionCredential(id.String(), ref.String())
	}
	if err := lifecycle.ReconcileStagedCredentials(t.Context(), time.Now().Add(48*time.Hour), cleanup); err != nil {
		t.Fatal(err)
	}
	if len(registry.StagedCredentials()) != 1 {
		t.Fatal("cleanup did not preserve live candidate only")
	}
	currentRevision, _ := domain.ParseRevision(replaced.Revision)
	if err := commands.Delete(t.Context(), id, currentRevision, "local", admit); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ReconcileStagedCredentials(t.Context(), time.Now().Add(48*time.Hour), cleanup); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ResolveScopedCredential(t.Context(), scope, ref.String()); err == nil {
		t.Fatal("unused material survived cleanup")
	}
	public, _ := json.Marshal(replaced)
	if strings.Contains(string(public), "synthetic-new-secret") || strings.Contains(string(public), "connections/") {
		t.Fatal("public response leaked material or locator")
	}
}

func TestSecretReplacementReusesMaterialAndOriginalResultAfterRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SWOBU_HOME", filepath.Join(dir, "home"))
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	registry, err := connectionfixture.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := configstore.OpenOrCreate(filepath.Join(dir, "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer workspaces.Close()
	lifecycle, err := domain.NewLifecycle(registry, workspaces)
	if err != nil {
		t.Fatal(err)
	}
	commands, _ := NewCommands(registry, lifecycle)
	endpoint, _ := domain.ParseEndpoint("https://example.com/mcp")
	spec, _ := domain.NewMCPHTTPSpec(endpoint, credentialref.Parse("env:ORIGINAL"))
	admit := func(context.Context) error { return nil }
	created, _, err := commands.Create(t.Context(), "Shared", spec, "local", "create", admit)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.Parse(created.ID)
	revision, _ := domain.ParseRevision(created.Revision)
	writes, freshWrites := 0, 0
	slot := ""
	writer := func(_ context.Context, id connectionidentity.ID, selected, secret string, fresh bool) (credentialref.Ref, error) {
		writes++
		if fresh {
			freshWrites++
		}
		if slot != "" && slot != selected {
			t.Fatal("retry allocated another slot")
		}
		slot = selected
		raw, err := credentials.MaterializeConnectionCredential(id.String(), selected, secret, credentials.CredentialWritePolicyFile, fresh)
		return credentialref.Parse(raw), err
	}
	// Storage succeeds, then authority is withdrawn at publication. The next
	// command must reuse the durably reserved slot instead of allocating again.
	withdrawn := false
	failedWriter := func(ctx context.Context, id connectionidentity.ID, slot, secret string, fresh bool) (credentialref.Ref, error) {
		ref, err := writer(ctx, id, slot, secret, fresh)
		withdrawn = true
		return ref, err
	}
	guarded := func(context.Context) error {
		if withdrawn {
			return errors.New("withdrawn after storage")
		}
		return nil
	}
	if _, _, err := commands.ReplaceCredential(t.Context(), id, revision, "", "synthetic-material", true, "local", "replace", guarded, failedWriter); err == nil {
		t.Fatal("withdrawn publication succeeded")
	}
	original, _ := registry.Get(id)
	if original.Revision() != revision || len(registry.Audits()) != 1 {
		t.Fatal("failed publication changed config or audit")
	}
	reopenedBeforeCommit, err := connectionfixture.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	commands.registry = reopenedBeforeCommit
	first, replay, err := commands.ReplaceCredential(t.Context(), id, revision, "", "synthetic-material", true, "local", "replace", admit, writer)
	if err != nil || replay || first.Revision != "2" {
		t.Fatal("replacement failed", err)
	}
	current, _ := commands.registry.Get(id)
	if _, err := commands.EditSameIdentity(t.Context(), id, current.Revision(), "Later", current.Spec(), true, "local", admit); err != nil {
		t.Fatal(err)
	}
	// Reopen the registry without installing a second Workspace validator.
	reopened, err := connectionfixture.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	commands.registry = reopened
	result, replay, err := commands.ReplaceCredential(t.Context(), id, revision, "", "synthetic-material", true, "local", "replace", admit, writer)
	if err != nil || !replay || result.Revision != first.Revision || result.DisplayName != first.DisplayName || freshWrites != 1 {
		t.Fatal("restart retry failed", err)
	}
	if _, _, err := commands.ReplaceCredential(t.Context(), id, revision, "", "synthetic-changed", true, "local", "replace", admit, writer); !errors.Is(err, credentials.ErrConnectionMaterialConflict) {
		t.Fatal("changed material retry accepted", err)
	}
	called := writes
	denied := errors.New("denied")
	if _, _, err := commands.ReplaceCredential(t.Context(), id, revision, "", "synthetic-material", true, "local", "replace", func(context.Context) error { return denied }, writer); !errors.Is(err, denied) || writes != called {
		t.Fatal("denied replay stored material")
	}
	if _, _, err := commands.ReplaceCredential(t.Context(), id, revision, "", "synthetic-material", false, "local", "replace", admit, writer); !errors.Is(err, ErrIdentityAssertion) || writes != called {
		t.Fatal("missing identity assertion stored material")
	}
	raw, _ := os.ReadFile(path)
	public, _ := json.Marshal(first)
	for _, secret := range []string{"synthetic-material", "synthetic-changed"} {
		if strings.Contains(string(raw), secret) || strings.Contains(string(public), secret) {
			t.Fatal("secret escaped credential owner")
		}
	}
	if len(reopened.Audits()) != 3 || len(reopened.StagedCredentials()) != 2 {
		t.Fatal("retry added facts or stages")
	}
}
