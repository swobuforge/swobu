package configstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestCheckedDisplayNameConsentAdmissionAndNoOp(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	original, _ := store.GetWorkspace(t.Context(), slug)
	etag := func(w routing.Workspace) string {
		return fmt.Sprintf("%s-%d", w.ManagementIncarnation(), w.ManagementRevision())
	}
	admit := func(context.Context) error { return nil }
	denied := errors.New("withdrawn")
	before, _ := os.ReadFile(path)
	if _, err := store.SetWorkspaceDisplayNameChecked(t.Context(), slug, "changed", etag(original), func(context.Context) error { return denied }); !errors.Is(err, denied) {
		t.Fatal("admission bypassed")
	}
	if _, err := store.SetWorkspaceDisplayNameChecked(t.Context(), slug, "changed", "stale", admit); !errors.Is(err, ErrStaleRevision) {
		t.Fatal("stale consent accepted")
	}
	if _, err := store.SetWorkspaceDisplayNameChecked(t.Context(), slug, "", etag(original), admit); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("no-op/rejection republished bytes")
	}
	rename := store.ops.rename
	store.ops.rename = func(string, string) error { return errors.New("publication failure") }
	if _, err := store.SetWorkspaceDisplayNameChecked(t.Context(), slug, "Research", etag(original), admit); err == nil {
		t.Fatal("failed publication accepted")
	}
	store.ops.rename = rename
	current, _ := store.GetWorkspace(t.Context(), slug)
	if current.DisplayName() != "" || etag(current) != etag(original) {
		t.Fatal("failed publication changed consent")
	}
	committed, err := store.SetWorkspaceDisplayNameChecked(t.Context(), slug, "Research", etag(original), admit)
	if err != nil || committed.DisplayName() != "Research" || committed.Slug() != slug || committed.ManagementIncarnation() != original.ManagementIncarnation() {
		t.Fatal("committed snapshot lost scope")
	}
	if _, err := store.SetWorkspaceDisplayNameChecked(t.Context(), slug, "Research", etag(original), admit); !errors.Is(err, ErrStaleRevision) {
		t.Fatal("no-op bypassed stale consent")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.SetWorkspaceDisplayNameChecked(t.Context(), slug, "other", etag(original), admit); !errors.Is(err, ErrStaleRevision) {
		t.Fatal("restart revived consent")
	}
	before, _ = os.ReadFile(path)
	if _, err := reopened.SetWorkspaceDisplayNameChecked(t.Context(), slug, "Research", etag(committed), admit); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("reopened no-op wrote bytes")
	}
}

func TestWorkspaceDisplayNamePreservesIdentityAcrossPublicationAndReopen(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	original, err := store.GetWorkspace(t.Context(), slug)
	if err != nil {
		t.Fatal(err)
	}
	edit := func(c routing.Config) (routing.Config, error) {
		return c.SetWorkspaceDisplayName(slug, "  Research 東京  ")
	}
	before, _ := os.ReadFile(path)
	rename := store.ops.rename
	store.ops.rename = func(string, string) error { return errors.New("injected publication failure") }
	if _, err := store.Update(t.Context(), edit); err == nil {
		t.Fatal("failed publication accepted")
	}
	after, _ := os.ReadFile(path)
	current, _ := store.GetWorkspace(t.Context(), slug)
	if !bytes.Equal(before, after) || current.DisplayName() != "" || current.ManagementRevision() != original.ManagementRevision() {
		t.Fatal("failed edit changed disk/memory/revision")
	}
	store.ops.rename = rename
	if _, err := store.Update(t.Context(), edit); err != nil {
		t.Fatal(err)
	}
	current, err = store.GetWorkspace(t.Context(), slug)
	if err != nil || current.DisplayName() != "Research 東京" || current.Slug() != original.Slug() || current.ManagementIncarnation() != original.ManagementIncarnation() || current.ManagementRevision() != original.ManagementRevision()+1 {
		t.Fatal("display edit changed identity or lost metadata")
	}
	for _, schema := range []int{routing.ReferenceSchemaVersion} {
		raw, err := encodeDocument(store.Config(), schema)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		workspace, _ := decoded.Workspace(slug)
		if workspace.DisplayName() != current.DisplayName() || workspace.Slug() != slug {
			t.Fatal("schema roundtrip lost display metadata")
		}
	}
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		return c.SetServiceEnabled(slug, routing.ServiceSearch, true)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) { return c.SetSearchRoute(slug, "search-fixture", nil) }); err != nil {
		t.Fatal(err)
	}
	current, _ = store.GetWorkspace(t.Context(), slug)
	if current.DisplayName() != "Research 東京" {
		t.Fatal("capability edit erased display name")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	current, err = reopened.GetWorkspace(t.Context(), slug)
	if err != nil || current.DisplayName() != "Research 東京" || current.Slug() != slug || current.ManagementIncarnation() != original.ManagementIncarnation() {
		t.Fatal("reopen changed display/identity")
	}
}

func TestWorkspaceDisplayNameRejectsInvalidInputWithoutPublication(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	before, _ := os.ReadFile(path)
	for _, name := range []string{"bad\nname", "bad\x00name", strings.Repeat("界", 129), string([]byte{0xff})} {
		if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) { return c.SetWorkspaceDisplayName(slug, name) }); err == nil {
			t.Fatal("invalid display accepted")
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid edit published")
	}
}
