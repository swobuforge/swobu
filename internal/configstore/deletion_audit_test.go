package configstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
	"gopkg.in/yaml.v3"
)

func TestCheckedDeletionAuditPreservesOriginalLifetimeAfterReplayEvictionAndReopen(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	workspace, _ := store.GetWorkspace(t.Context(), slug)
	etag := fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision())
	admit := func(context.Context) error { return nil }
	before, _ := os.ReadFile(path)
	rename := store.ops.rename
	store.ops.rename = func(string, string) error { return errors.New("publication failed") }
	if _, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "trusted-caller", "original-key", admit, nil); err == nil {
		t.Fatal("failed deletion acknowledged")
	}
	if facts, err := store.WorkspaceDeletionAudits(); err != nil || len(facts) != 0 {
		t.Fatal("failed deletion retained facts", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed deletion changed authority")
	}
	store.ops.rename = rename
	if replayed, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "trusted-caller", "original-key", admit, nil); err != nil || replayed {
		t.Fatal("fresh deletion failed", err)
	}
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	before, _ = os.ReadFile(path)
	if replayed, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "trusted-caller", "original-key", admit, nil); err != nil || !replayed {
		t.Fatal("historical replay lost", err)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("replay changed replacement or facts")
	}
	facts, err := store.WorkspaceDeletionAudits()
	if err != nil || len(facts) != 1 || facts[0].ETag != etag || facts[0].Workspace != "dev" || facts[0].Caller != "trusted-caller" {
		t.Fatal("original deletion attribution lost", err)
	}
	facts[0].Caller = "tampered"
	store.deletions = nil
	if _, err := store.Update(t.Context(), func(config routing.Config) (routing.Config, error) { return config, nil }); err != nil {
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
	if facts, err := store.WorkspaceDeletionAudits(); err != nil || len(facts) != 1 || facts[0].Caller != "trusted-caller" {
		t.Fatal("eviction/reopen erased audit", err)
	}
	raw, _ := os.ReadFile(path)
	migrated, err := reencodeCurrentDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	if facts, err := decodeWorkspaceDeletionAudits(migrated); err != nil || len(facts) != 1 {
		t.Fatal("migration erased audit", err)
	}
	for _, malformed := range [][]byte{
		bytes.Replace(raw, []byte("action: workspace.delete"), []byte("action: forged"), 1),
		bytes.Replace(raw, []byte("caller: trusted-caller"), []byte("caller: null"), 1),
		bytes.Replace(raw, []byte("workspace: dev"), []byte("workspace: invalid/scope"), 1),
	} {
		if bytes.Equal(raw, malformed) {
			t.Fatal("unchanged corruption fixture")
		}
		if _, err := decode(malformed); err == nil {
			t.Fatal("malformed deletion audit admitted")
		}
	}
}

func TestHistoricalDeletionReceiptNeverAcquiresAudit(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	workspace, _ := store.GetWorkspace(t.Context(), slug)
	etag := fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision())
	admit := func(context.Context) error { return nil }
	if _, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "trusted-caller", "original-key", admit, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var document documentDTO
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	for identity, audit := range document.DeletionAudits {
		audit.Caller = "conflicting-caller"
		document.DeletionAudits[identity] = audit
	}
	conflicting, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(conflicting); err == nil {
		t.Fatal("conflicting retained and receipt deletion facts admitted")
	}
	document.DeletionAudits = nil
	for key, receipt := range document.Deletions {
		receipt.Audit = nil
		document.Deletions[key] = receipt
	}
	historical, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
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
	if replayed, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "trusted-caller", "original-key", admit, nil); err != nil || !replayed {
		t.Fatal("historical replay failed", err)
	}
	if facts, err := store.WorkspaceDeletionAudits(); err != nil || len(facts) != 0 {
		t.Fatal("historical deletion acquired audit", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(historical, after) {
		t.Fatal("historical read rewrote bytes")
	}
	explicitNull := bytes.Replace(historical, []byte("        intent:"), []byte("        audit: null\n        intent:"), 1)
	if bytes.Equal(explicitNull, historical) {
		t.Fatal("unchanged null fixture")
	}
	if _, err := decode(explicitNull); err == nil {
		t.Fatal("null audit admitted as historical omission")
	}
}
