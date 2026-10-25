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

func TestCheckedDisplayNameAuditOnlyCommitsActualChangesAndPreservesOriginalReview(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	workspace, _ := store.GetWorkspace(t.Context(), slug)
	etag := fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision())
	admit := func(context.Context) error { return nil }
	before, _ := os.ReadFile(path)
	denied := errors.New("withdrawn")
	if _, err := store.SetWorkspaceDisplayNameChecked(t.Context(), slug, "Research", etag, func(context.Context) error { return denied }); !errors.Is(err, denied) {
		t.Fatal("admission bypassed")
	}
	if _, err := store.SetWorkspaceDisplayNameChecked(t.Context(), slug, "Research", "stale", admit); !errors.Is(err, ErrStaleRevision) {
		t.Fatal("stale review accepted")
	}
	if _, err := store.SetWorkspaceDisplayNameChecked(t.Context(), slug, "", etag, admit); err != nil {
		t.Fatal(err)
	}
	rename := store.ops.rename
	store.ops.rename = func(string, string) error { return errors.New("publication failed") }
	if _, err := store.SetWorkspaceDisplayNameChecked(t.Context(), slug, "Research", etag, admit); err == nil {
		t.Fatal("failed write acknowledged")
	}
	store.ops.rename = rename
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("denial/no-op/failure changed state")
	}
	if facts, err := store.WorkspaceDisplayNameAudits(); err != nil || len(facts) != 0 {
		t.Fatal("no change acquired facts", err)
	}
	updated, err := store.SetWorkspaceDisplayNameChecked(t.Context(), slug, "Research", etag, admit)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := store.WorkspaceDisplayNameAudits()
	if err != nil || len(facts) != 1 || facts[0].Before != "" || facts[0].After != "Research" || facts[0].ETag != etag || facts[0].Caller != "local" {
		t.Fatal("checked change lost original facts", err)
	}
	facts[0].After = "tampered"
	updatedETag := fmt.Sprintf("%s-%d", updated.ManagementIncarnation(), updated.ManagementRevision())
	if _, err := store.SetWorkspaceDisplayNameChecked(t.Context(), slug, "", updatedETag, admit); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(t.Context(), func(config routing.Config) (routing.Config, error) { return config.DeleteWorkspace(slug) }); err != nil {
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
	facts, err = store.WorkspaceDisplayNameAudits()
	if err != nil || len(facts) != 2 || facts[0].After != "Research" || facts[1].Before != "Research" || facts[1].After != "" {
		t.Fatal("reopen/removal lost detached history", err)
	}
	raw, _ := os.ReadFile(path)
	migrated, err := reencodeCurrentDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	if facts, err := decodeDisplayNameAudits(migrated); err != nil || len(facts) != 2 {
		t.Fatal("migration lost display facts", err)
	}
	for _, malformed := range [][]byte{
		bytes.Replace(raw, []byte("action: workspace.display_name"), []byte("action: other"), 1),
		bytes.Replace(raw, []byte("caller: local"), []byte("caller: foreign"), 1),
		bytes.Replace(raw, []byte("before: \"\""), []byte("before: null"), 1),
		bytes.Replace(raw, []byte("after: Research"), []byte("after: ''"), 1),
	} {
		if bytes.Equal(raw, malformed) {
			t.Fatal("corruption fixture unchanged")
		}
		if _, err := decode(malformed); err == nil {
			t.Fatal("malformed display audit admitted")
		}
	}
	var document documentDTO
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document.DisplayNameAudits = append(document.DisplayNameAudits, document.DisplayNameAudits[0])
	duplicate, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(duplicate); err == nil {
		t.Fatal("duplicate display review admitted")
	}
	document.DisplayNameAudits = nil
	historical, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if facts, err := decodeDisplayNameAudits(historical); err != nil || len(facts) != 0 {
		t.Fatal("historical configuration invented display facts", err)
	}
}
