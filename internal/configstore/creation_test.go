package configstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
	"gopkg.in/yaml.v3"
)

func creationDraft(t *testing.T, name string) routing.Workspace {
	t.Helper()
	slug, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	w, err := routing.RestoreWorkspace(slug, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, err = w.WithDisplayName(name)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestCheckedCreationRetainedWindowAndMalformedResult(t *testing.T) {
	store, path := openTestStore(t)
	draft := creationDraft(t, "Research")
	admit := func(context.Context) error { return nil }
	original, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "key", admit)
	if err != nil {
		t.Fatal(err)
	}
	var retained creationReceipt
	for _, receipt := range store.creations {
		retained = receipt
	}
	for i := 0; i < creationReceiptCapacity-1; i++ {
		store.creations[fmt.Sprintf("%064x", i)] = creationReceipt{Intent: retained.Intent, Result: retained.Result, ExpiresAt: time.Now().Add(creationReceiptTTL * 2)}
	}
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) { return c.DeleteWorkspace(draft.Slug()) }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "next", admit); err != nil {
		t.Fatal(err)
	}
	if len(store.creations) != creationReceiptCapacity {
		t.Fatalf("capacity=%d", len(store.creations))
	}
	if _, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "key", admit); !errors.Is(err, routing.ErrConflict) {
		t.Fatal("evicted receipt replayed", err)
	}
	for k, receipt := range store.creations {
		receipt.ExpiresAt = time.Now().Add(-time.Hour)
		store.creations[k] = receipt
	}
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) { return c.DeleteWorkspace(draft.Slug()) }); err != nil {
		t.Fatal(err)
	}
	fresh, replay, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "next", admit)
	if err != nil || replay || fresh.ManagementIncarnation() == original.ManagementIncarnation() || len(store.creations) != 1 {
		t.Fatal("expired replay window retained authority", err)
	}
	raw, _ := os.ReadFile(path)
	var dto documentDTO
	if err := yaml.Unmarshal(raw, &dto); err != nil {
		t.Fatal(err)
	}
	for k, receipt := range dto.Creations {
		receipt.Result = "schema_version: 5\nworkspaces: {}\n"
		dto.Creations[k] = receipt
	}
	malformed, err := yaml.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(malformed); err == nil {
		t.Fatal("malformed original result accepted")
	}
}

func TestCheckedCreationConcurrentRetryCommitsOneLifetime(t *testing.T) {
	store, _ := openTestStore(t)
	draft := creationDraft(t, "Research")
	var wg sync.WaitGroup
	results := make(chan routing.Workspace, 16)
	fresh := make(chan bool, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, replay, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "key", func(context.Context) error { return nil })
			if err != nil {
				t.Error(err)
				return
			}
			results <- result
			fresh <- !replay
		}()
	}
	wg.Wait()
	close(results)
	close(fresh)
	commits := 0
	for applied := range fresh {
		if applied {
			commits++
		}
	}
	current, _ := store.GetWorkspace(t.Context(), draft.Slug())
	for result := range results {
		if result.ManagementIncarnation() != current.ManagementIncarnation() || result.ManagementRevision() != current.ManagementRevision() {
			t.Fatal("receipt differs from committed snapshot")
		}
	}
	if commits != 1 || len(store.creations) != 1 {
		t.Fatalf("commits=%d receipts=%d", commits, len(store.creations))
	}
}

func TestCheckedCreationIntentIncludesResourceAndCanonicalMetadata(t *testing.T) {
	store, _ := openTestStore(t)
	draft := creationDraft(t, "  Research  ")
	admit := func(context.Context) error { return nil }
	original, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "key", admit)
	if err != nil {
		t.Fatal(err)
	}
	got, replay, err := store.CreateWorkspaceChecked(t.Context(), creationDraft(t, "Research"), "local", "key", admit)
	if err != nil || !replay || got.ManagementIncarnation() != original.ManagementIncarnation() {
		t.Fatal("equivalent validated intent did not replay", err)
	}
	slug, _ := routing.ParseWorkspaceSlug("other")
	other, err := routing.RestoreWorkspace(slug, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateWorkspaceChecked(t.Context(), other, "local", "key", admit); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("key reused for different resource", err)
	}
	if _, exists := store.Config().Workspace(slug); exists {
		t.Fatal("changed resource created")
	}
}

func TestCheckedCreationPreservesComposedDraft(t *testing.T) {
	store, path := openTestStore(t)
	draft := creationDraft(t, "Research")
	composed, err := routing.RestoreWorkspaceWithSearchConnection(draft.Slug(), nil, nil, nil, "search-source")
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := store.CreateWorkspaceChecked(t.Context(), composed, "local", "key", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if created.SearchConnectionID() != "search-source" {
		t.Fatal("creation lost capability")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	config, err := ValidateDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	if config.Workspaces()[0].SearchConnectionID() != "search-source" {
		t.Fatal("publication lost capability")
	}
}

func TestCheckedCreationCancellationAndClosedStoreHaveNoEffect(t *testing.T) {
	store, path := openTestStore(t)
	before, _ := os.ReadFile(path)
	ctx, cancel := context.WithCancel(t.Context())
	_, _, err := store.CreateWorkspaceChecked(ctx, creationDraft(t, "Research"), "local", "key", func(context.Context) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled admission applied", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || len(store.creations) != 0 {
		t.Fatal("cancelled creation published")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateWorkspaceChecked(t.Context(), creationDraft(t, "Research"), "local", "key", func(context.Context) error { return nil }); !errors.Is(err, ErrStoreClosed) {
		t.Fatal("closed store applied", err)
	}
}

func TestCheckedCreationRetainsOriginalResultAcrossEditRecreateAndReopen(t *testing.T) {
	store, path := openTestStore(t)
	draft := creationDraft(t, "Research")
	admissions := 0
	admit := func(context.Context) error { admissions++; return nil }
	original, replay, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "original-key", admit)
	if err != nil || replay || original.ManagementIncarnation() == "" || original.ManagementRevision() != 1 {
		t.Fatalf("creation replay=%v err=%v", replay, err)
	}
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		return c.SetWorkspaceDisplayName(draft.Slug(), "Later")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) { return c.DeleteWorkspace(draft.Slug()) }); err != nil {
		t.Fatal(err)
	}
	replacement, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "replacement-key", admit)
	if err != nil || replacement.ManagementIncarnation() == original.ManagementIncarnation() {
		t.Fatal("replacement reused lifetime", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	before, _ := os.ReadFile(path)
	got, replay, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "original-key", admit)
	if err != nil || !replay || got.ManagementIncarnation() != original.ManagementIncarnation() || got.DisplayName() != "Research" || got.ManagementRevision() != 1 {
		t.Fatal("original creation result lost", err)
	}
	if _, _, err := store.CreateWorkspaceChecked(t.Context(), creationDraft(t, "Changed"), "local", "original-key", admit); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("changed intent accepted", err)
	}
	if _, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "foreign", "original-key", admit); !errors.Is(err, routing.ErrConflict) {
		t.Fatal("foreign scope replayed", err)
	}
	if _, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "original-key", func(context.Context) error { return errors.New("revoked") }); err == nil {
		t.Fatal("replay bypassed admission")
	}
	after, _ := os.ReadFile(path)
	current, _ := store.GetWorkspace(t.Context(), draft.Slug())
	if !bytes.Equal(before, after) || current.ManagementIncarnation() != replacement.ManagementIncarnation() || admissions != 5 {
		t.Fatal("replay published or changed replacement")
	}
	migrated, err := reencodeCurrentDocument(after)
	if err != nil {
		t.Fatal(err)
	}
	receipts, err := decodeCreationReceipts(migrated)
	if err != nil || len(receipts) != 2 {
		t.Fatal("migration lost creation receipts", err)
	}
	if bytes.Contains(after, []byte("original-key")) || bytes.Contains(after, []byte("replacement-key")) {
		t.Fatal("raw retry key persisted")
	}
}

func TestCheckedCreationFailedPublicationHasNoReceiptOrPartialWorkspace(t *testing.T) {
	store, path := openTestStore(t)
	before, _ := os.ReadFile(path)
	rename := store.ops.rename
	store.ops.rename = func(string, string) error { return errors.New("publication failed") }
	draft := creationDraft(t, "Research")
	admit := func(context.Context) error { return nil }
	if _, _, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "key", admit); err == nil {
		t.Fatal("publication succeeded")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || len(store.Config().Workspaces()) != 0 || len(store.creations) != 0 {
		t.Fatal("failed commit published state or receipt")
	}
	store.ops.rename = rename
	if _, replay, err := store.CreateWorkspaceChecked(t.Context(), draft, "local", "key", admit); err != nil || replay {
		t.Fatal("uncommitted retry mistaken for replay", err)
	}
}
