package configstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestCheckedDeletionCommitsReceiptAndReplaySkipsRecreatedWorkspace(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	// Ordinary edits retain the receipt; schema migration must do so too.
	slug, _ := routing.ParseWorkspaceSlug("dev")
	w, _ := store.GetWorkspace(t.Context(), slug)
	etag := fmt.Sprintf("%s-%d", w.ManagementIncarnation(), w.ManagementRevision())
	admissions, effects := 0, 0
	authorize := func(context.Context) error { admissions++; return nil }
	effect := func(routing.Config, routing.Config) error { effects++; return nil }
	if replay, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "key", authorize, effect); err != nil || replay {
		t.Fatalf("apply replay=%t err=%v", replay, err)
	}
	if _, ok := store.Config().Workspace(slug); ok {
		t.Fatal("not deleted")
	}
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if replay, err := reopened.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "key", authorize, effect); err != nil || !replay {
		t.Fatalf("replay=%t err=%v", replay, err)
	}
	if _, ok := reopened.Config().Workspace(slug); !ok || effects != 1 || admissions != 2 {
		t.Fatal("replay touched recreated Workspace or bypassed admission")
	}
	if _, err := reopened.DeleteWorkspaceChecked(t.Context(), slug, etag+"changed", "local", "key", authorize, effect); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed intent: %v", err)
	}
	if _, err := reopened.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "new-key", authorize, effect); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale incarnation: %v", err)
	}
	if effects != 1 {
		t.Fatal("stale admission invoked revocation")
	}
	if _, err := reopened.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "key", func(context.Context) error { return errors.New("revoked") }, effect); err == nil {
		t.Fatal("revoked replay admitted")
	}
	raw, _ := os.ReadFile(path)
	migrated, err := reencodeCurrentDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	receipts, err := decodeDeletionReceipts(migrated)
	if err != nil || len(receipts) != 1 {
		t.Fatal("migration dropped deletion receipt")
	}
	if bytes.Contains(raw, []byte("new-key")) || bytes.Contains(raw, []byte("expired-key")) {
		t.Fatal("raw idempotency key persisted")
	}
}

func TestCheckedDeletionFailedPublicationDoesNotPublishReceipt(t *testing.T) {
	store, _ := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	w, _ := store.GetWorkspace(t.Context(), slug)
	etag := fmt.Sprintf("%s-%d", w.ManagementIncarnation(), w.ManagementRevision())
	rename := store.ops.rename
	store.ops.rename = func(string, string) error { return errors.New("injected publication failure") }
	effects := 0
	effect := func(routing.Config, routing.Config) error { effects++; return nil }
	authorize := func(context.Context) error { return nil }
	if _, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "key", authorize, effect); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	if _, ok := store.Config().Workspace(slug); !ok || len(store.deletions) != 0 || effects != 1 {
		t.Fatal("failed publication altered routing/receipt or skipped precommit effect")
	}
	store.ops.rename = rename
	if replay, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "key", authorize, effect); err != nil || replay {
		t.Fatalf("retry replay=%t err=%v", replay, err)
	}
	if effects != 2 || len(store.deletions) != 1 {
		t.Fatal("retry failed to commit")
	}
}

func TestDeletionReceiptExpiryAndGrammar(t *testing.T) {
	store, _ := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	w, _ := store.GetWorkspace(t.Context(), slug)
	etag := fmt.Sprintf("%s-%d", w.ManagementIncarnation(), w.ManagementRevision())
	authorize := func(context.Context) error { return nil }
	if _, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "key", authorize, nil); err != nil {
		t.Fatal(err)
	}
	for key, receipt := range store.deletions {
		receipt.ExpiresAt = time.Now().Add(-time.Hour)
		receipt.Audit = nil
		store.deletions[key] = receipt
	}
	if _, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "key", authorize, nil); !errors.Is(err, routing.ErrNotFound) {
		t.Fatalf("expired receipt replayed: %v", err)
	}
	if _, err := decodeDeletionReceipts([]byte("workspace_deletions:\n  invalid:\n    intent: invalid\n    expires_at: 2026-10-11T00:00:00Z\n")); err == nil {
		t.Fatal("invalid receipt accepted")
	}
}

func TestCheckedDeletionCapacityAndCancellation(t *testing.T) {
	store, _ := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	w, _ := store.GetWorkspace(t.Context(), slug)
	etag := fmt.Sprintf("%s-%d", w.ManagementIncarnation(), w.ManagementRevision())
	ctx, cancel := context.WithCancel(t.Context())
	if _, err := store.DeleteWorkspaceChecked(ctx, slug, etag, "local", "cancel", func(context.Context) error { cancel(); return nil }, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel during admission: %v", err)
	}
	if _, ok := store.Config().Workspace(slug); !ok {
		t.Fatal("cancelled admission deleted")
	}
	store.deletions = make(map[string]deletionReceipt)
	for i := 0; i < deletionReceiptCapacity; i++ {
		store.deletions[fmt.Sprintf("%064x", i)] = deletionReceipt{Intent: fmt.Sprintf("%064x", i), ExpiresAt: time.Now().Add(time.Hour)}
	}
	if _, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "key", func(context.Context) error { return nil }, nil); err != nil {
		t.Fatal(err)
	}
	if len(store.deletions) != deletionReceiptCapacity {
		t.Fatal("receipt capacity exceeded")
	}
}

func TestDeletionReceiptEvictionPublishesAtomicallyAndCannotDeleteRecreatedLifetime(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	original, _ := store.GetWorkspace(t.Context(), slug)
	etag := fmt.Sprintf("%s-%d", original.ManagementIncarnation(), original.ManagementRevision())
	allow := func(context.Context) error { return nil }
	if _, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "original-key", allow, nil); err != nil {
		t.Fatal(err)
	}
	var originalHash string
	for key := range store.deletions {
		originalHash = key
	}
	// Saturate the documented window with later-expiring receipts. The original
	// is the oldest candidate, so the next successful publication must evict it.
	later := time.Now().Add(48 * time.Hour)
	for i := 0; i < deletionReceiptCapacity-1; i++ {
		key := fmt.Sprintf("%064x", i)
		if key == originalHash {
			t.Fatal("fixture digest collision")
		}
		store.deletions[key] = deletionReceipt{Intent: key, ExpiresAt: later}
	}
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	replacement, _ := store.GetWorkspace(t.Context(), slug)
	if replacement.ManagementIncarnation() == original.ManagementIncarnation() {
		t.Fatal("recreated lifetime reused")
	}
	if _, err := store.Update(t.Context(), addWorkspace(t, "other")); err != nil {
		t.Fatal(err)
	}
	otherSlug, _ := routing.ParseWorkspaceSlug("other")
	other, _ := store.GetWorkspace(t.Context(), otherSlug)
	otherETag := fmt.Sprintf("%s-%d", other.ManagementIncarnation(), other.ManagementRevision())
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rename := store.ops.rename
	store.ops.rename = func(string, string) error { return errors.New("blocked retention publication") }
	if _, err := store.DeleteWorkspaceChecked(t.Context(), otherSlug, otherETag, "local", "next-key", allow, nil); err == nil {
		t.Fatal("blocked publication succeeded")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed eviction publication changed disk")
	}
	if _, ok := store.deletions[originalHash]; !ok {
		t.Fatal("failed publication evicted retained receipt")
	}
	if _, ok := store.Config().Workspace(otherSlug); !ok {
		t.Fatal("failed publication removed Workspace")
	}
	store.ops.rename = rename
	if replay, err := store.DeleteWorkspaceChecked(t.Context(), otherSlug, otherETag, "local", "next-key", allow, nil); err != nil || replay {
		t.Fatalf("repaired publication: replay=%t err=%v", replay, err)
	}
	if _, ok := store.deletions[originalHash]; ok || len(store.deletions) != deletionReceiptCapacity {
		t.Fatal("successful publication did not enforce capacity")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	effects := 0
	if _, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "original-key", allow, func(routing.Config, routing.Config) error { effects++; return nil }); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("evicted old intent=%v", err)
	}
	current, ok := store.Config().Workspace(slug)
	if !ok || current.ManagementIncarnation() != replacement.ManagementIncarnation() || effects != 0 {
		t.Fatal("evicted receipt authorized replacement destruction")
	}
	if replay, err := store.DeleteWorkspaceChecked(t.Context(), otherSlug, otherETag, "local", "next-key", allow, nil); err != nil || !replay {
		t.Fatalf("new retained receipt lost on reopen: %v", err)
	}
}

func TestExpiredDeletionReceiptAfterReopenCannotDeleteRecreatedLifetime(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	original, _ := store.GetWorkspace(t.Context(), slug)
	etag := fmt.Sprintf("%s-%d", original.ManagementIncarnation(), original.ManagementRevision())
	allow := func(context.Context) error { return nil }
	if _, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "expired-key", allow, nil); err != nil {
		t.Fatal(err)
	}
	for key, receipt := range store.deletions {
		receipt.ExpiresAt = time.Now().Add(-time.Hour)
		receipt.Audit = nil
		store.deletions[key] = receipt
	}
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	effects := 0
	if _, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "local", "expired-key", allow, func(routing.Config, routing.Config) error { effects++; return nil }); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("expired original intent=%v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || effects != 0 {
		t.Fatal("expired stale intent published or revoked authority")
	}
}

func TestDeletionReceiptCallerScopeCannotReplayAnotherCallersReceipt(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	original, _ := store.GetWorkspace(t.Context(), slug)
	etag := fmt.Sprintf("%s-%d", original.ManagementIncarnation(), original.ManagementRevision())
	allow := func(context.Context) error { return nil }
	if _, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "caller-one", "same-key", allow, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	effects := 0
	effect := func(routing.Config, routing.Config) error { effects++; return nil }
	if _, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "caller-two", "same-key", allow, effect); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("foreign receipt replayed: %v", err)
	}
	if replay, err := store.DeleteWorkspaceChecked(t.Context(), slug, etag, "caller-one", "same-key", allow, effect); err != nil || !replay {
		t.Fatalf("original caller lost replay: %v", err)
	}
	if effects != 0 {
		t.Fatal("foreign or matching replay invoked effect")
	}
	current, _ := store.GetWorkspace(t.Context(), slug)
	currentETag := fmt.Sprintf("%s-%d", current.ManagementIncarnation(), current.ManagementRevision())
	if replay, err := store.DeleteWorkspaceChecked(t.Context(), slug, currentETag, "caller-two", "same-key", allow, effect); err != nil || replay {
		t.Fatalf("independent caller key conflicted: %v", err)
	}
	if effects != 1 || len(store.deletions) != 2 {
		t.Fatal("caller namespaces did not retain separate effects/receipts")
	}
}
