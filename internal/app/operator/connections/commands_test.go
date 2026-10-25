package connections

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/configstore"
	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

func TestCommandsCreateAndEditIdentityAdmissionAndRedaction(t *testing.T) {
	registry, err := connectionfixture.Open(filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	workspaceDir := t.TempDir()
	if err := os.Chmod(workspaceDir, 0700); err != nil {
		t.Fatal(err)
	}
	workspaces, err := configstore.OpenOrCreate(filepath.Join(workspaceDir, "routing.yaml"))
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
	spec, _ := domain.NewMCPHTTPSpec(endpoint, credentialref.Parse("env:PRIVATE_LOCATOR"))
	admit := func(context.Context) error { return nil }
	created, replayed, err := commands.Create(t.Context(), "Account", spec, "operator", "retry", admit)
	if err != nil || replayed {
		t.Fatalf("create: %v", err)
	}
	id, _ := connectionidentity.Parse(created.ID)
	revision, _ := domain.ParseRevision(created.Revision)
	if _, err := commands.EditSameIdentity(t.Context(), id, revision, "Changed", spec, false, "operator", admit); !errors.Is(err, ErrIdentityAssertion) {
		t.Fatalf("missing identity assertion: %v", err)
	}
	denied := errors.New("denied")
	if _, err := commands.EditSameIdentity(t.Context(), id, revision, "Changed", spec, true, "operator", func(context.Context) error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("admission: %v", err)
	}
	edited, err := commands.EditSameIdentity(t.Context(), id, revision, "Changed", spec, true, "operator", admit)
	if err != nil || edited.ID != created.ID || edited.Revision != "2" {
		t.Fatalf("edit: %v %v", edited, err)
	}
	if _, err := commands.EditSameIdentity(t.Context(), id, revision, "Stale", spec, true, "operator", admit); !errors.Is(err, domain.ErrRevisionConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	original, replayed, err := commands.Create(t.Context(), "Account", spec, "operator", "retry", admit)
	if err != nil || !replayed || original.Revision != "1" {
		t.Fatal("replay returned edited state")
	}
	editedRevision, _ := domain.ParseRevision(edited.Revision)
	if err := commands.Delete(t.Context(), id, revision, "operator", admit); !errors.Is(err, domain.ErrRevisionConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	if err := commands.Delete(t.Context(), id, editedRevision, "operator", func(context.Context) error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("denied delete: %v", err)
	}
	if err := commands.Delete(t.Context(), id, editedRevision, "operator", admit); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Get(id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted identity remains: %v", err)
	}
	audits := registry.Audits()
	if len(audits) != 3 || audits[2].Action != "connection.delete" {
		t.Fatal("delete command lost management fact")
	}
	replayAfterDelete, replayed, err := commands.Create(t.Context(), "Account", spec, "operator", "retry", admit)
	if err != nil || !replayed || replayAfterDelete.ID != created.ID || len(registry.List()) != 0 {
		t.Fatal("create replay recreated deleted identity")
	}
	raw, _ := json.Marshal([]Connection{created, edited, original})
	if strings.Contains(string(raw), "PRIVATE_LOCATOR") {
		t.Fatal("command response leaked credential locator")
	}
}

func TestReplacementCommandRequiresIdentityAndRedactsReplay(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := connectionfixture.Open(filepath.Join(dir, "swobu.yaml"), nil)
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
	old := credentialref.Parse("env:PRIVATE_OLD")
	next := credentialref.Parse("secretfile:PRIVATE_NEW")
	spec, _ := domain.NewMCPHTTPSpec(endpoint, old)
	admit := func(context.Context) error { return nil }
	created, _, err := commands.Create(t.Context(), "Account", spec, "local", "create", admit)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.Parse(created.ID)
	revision, _ := domain.ParseRevision(created.Revision)
	if _, _, err := commands.ReplaceCredentialBinding(t.Context(), id, revision, "", old, next, false, "local", "replace", admit); !errors.Is(err, ErrIdentityAssertion) {
		t.Fatalf("identity assertion: %v", err)
	}
	result, replayed, err := commands.ReplaceCredentialBinding(t.Context(), id, revision, "", old, next, true, "local", "replace", admit)
	if err != nil || replayed || result.Revision != "2" {
		t.Fatalf("replace: %v", err)
	}
	original, replayed, err := commands.ReplaceCredentialBinding(t.Context(), id, revision, "", old, next, true, "local", "replace", admit)
	if err != nil || !replayed || original.Revision != "2" {
		t.Fatalf("replay: %v", err)
	}
	raw, _ := json.Marshal([]Connection{result, original})
	if strings.Contains(string(raw), "PRIVATE_") {
		t.Fatal("replacement response disclosed locator")
	}
}
