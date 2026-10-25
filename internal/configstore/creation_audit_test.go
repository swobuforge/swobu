package configstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
	"gopkg.in/yaml.v3"
)

func TestCheckedCreationAuditFailurePublishesNothingAndExpiryPreservesFacts(t *testing.T) {
	store, path := openTestStore(t)
	draft := creationDraft(t, "Research")
	admit := func(context.Context) error { return nil }
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rename := store.ops.rename
	store.ops.rename = func(string, string) error { return errors.New("publication failed") }
	if _, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "trusted-caller", "original-key", admit); err == nil {
		t.Fatal("failed creation acknowledged")
	}
	if facts, err := store.WorkspaceCreationAudits(); err != nil || len(facts) != 0 {
		t.Fatal("failed publication retained audit", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed publication changed bytes", err)
	}
	store.ops.rename = rename
	if _, replayed, err := store.CreateWorkspaceChecked(t.Context(), draft, "trusted-caller", "original-key", admit); err != nil || replayed {
		t.Fatal("failed write consumed intent", err)
	}
	for key, receipt := range store.creations {
		receipt.ExpiresAt = time.Now().Add(-time.Hour)
		store.creations[key] = receipt
	}
	if facts, err := store.WorkspaceCreationAudits(); err != nil || len(facts) != 1 {
		t.Fatal("expired replay envelope removed audit", err)
	}
	store.creations = nil
	if _, err := store.Update(t.Context(), func(config routing.Config) (routing.Config, error) { return config.DeleteWorkspace(draft.Slug()) }); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if facts, err := store.WorkspaceCreationAudits(); err != nil || len(facts) != 1 || facts[0].Workspace != "research" {
		t.Fatal("receipt eviction/reopen erased audit", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := reencodeCurrentDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	if facts, err := decodeWorkspaceCreationAudits(migrated); err != nil || len(facts) != 1 {
		t.Fatal("migration erased retained audit", err)
	}
	for _, malformed := range [][]byte{
		bytes.Replace(raw, []byte("action: workspace.create"), []byte("action: forged"), 1),
		bytes.Replace(raw, []byte("workspace: research"), []byte("workspace: foreign/invalid"), 1),
		bytes.Replace(raw, []byte("caller: trusted-caller"), []byte("caller: null"), 1),
	} {
		if bytes.Equal(raw, malformed) {
			t.Fatal("corruption fixture unchanged")
		}
		if _, err := decode(malformed); err == nil {
			t.Fatal("malformed retained audit admitted")
		}
	}
}

func TestCheckedCreationAuditCommitsOriginalLifetimeAndNeverInventsHistoricalFacts(t *testing.T) {
	store, path := openTestStore(t)
	draft := creationDraft(t, "Research")
	admit := func(context.Context) error { return nil }
	created, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "trusted-caller", "original-key", admit)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := store.WorkspaceCreationAudits()
	if err != nil || len(facts) != 1 || facts[0].Caller != "trusted-caller" || facts[0].Workspace != "research" || facts[0].Incarnation != created.ManagementIncarnation() || facts[0].Action != "workspace.create" || facts[0].CommittedAt.IsZero() {
		t.Fatal("creation omitted original facts", err)
	}
	facts[0].Caller = "tampered"
	if _, err := store.Update(t.Context(), func(config routing.Config) (routing.Config, error) { return config.DeleteWorkspace(draft.Slug()) }); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, malformed := range [][]byte{
		bytes.Replace(before, []byte("caller: trusted-caller"), []byte("caller: ''"), 1),
		bytes.Replace(before, []byte("action: workspace.create"), []byte("action: other"), 1),
		bytes.Replace(before, []byte("workspace: research"), []byte("workspace: foreign"), 1),
		bytes.Replace(before, []byte("caller: trusted-caller"), []byte("caller: trusted-caller\n            unknown: true"), 1),
	} {
		if _, err := decode(malformed); err == nil {
			t.Fatal("malformed creation audit admitted")
		}
	}
	if original, replayed, err := store.CreateWorkspaceChecked(t.Context(), draft, "trusted-caller", "original-key", admit); err != nil || !replayed || original.ManagementIncarnation() != created.ManagementIncarnation() {
		t.Fatal("original lifetime lost", err)
	}
	facts, err = store.WorkspaceCreationAudits()
	if err != nil || len(facts) != 1 || facts[0].Caller != "trusted-caller" {
		t.Fatal("replay duplicated or aliased audit", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("replay rewrote audit", err)
	}
	denied := errors.New("withdrawn")
	if _, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "trusted-caller", "original-key", func(context.Context) error { return denied }); !errors.Is(err, denied) {
		t.Fatal("audit bypassed admission", err)
	}
	var document documentDTO
	if err := yaml.Unmarshal(before, &document); err != nil {
		t.Fatal(err)
	}
	for identity, audit := range document.CreationAudits {
		audit.Caller = "conflicting-caller"
		document.CreationAudits[identity] = audit
	}
	conflict, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(conflict); err == nil {
		t.Fatal("conflicting retained and receipt facts admitted")
	}
	for key, receipt := range document.Creations {
		receipt.Audit = nil
		document.Creations[key] = receipt
	}
	document.CreationAudits = nil
	historical, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	explicitNull := bytes.Replace(historical, []byte("        intent:"), []byte("        audit: null\n        intent:"), 1)
	if bytes.Equal(explicitNull, historical) {
		t.Fatal("null audit fixture did not change document")
	}
	if _, err := decode(explicitNull); err == nil {
		t.Fatal("null audit treated as historical omission")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, historical, 0600); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, replayed, err := store.CreateWorkspaceChecked(t.Context(), draft, "trusted-caller", "original-key", admit); err != nil || !replayed {
		t.Fatal("historical replay rejected", err)
	}
	if facts, err := store.WorkspaceCreationAudits(); err != nil || len(facts) != 0 {
		t.Fatal("historical receipt acquired audit", err)
	}
	after, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(historical, after) {
		t.Fatal("historical read rewrote bytes", err)
	}
}
