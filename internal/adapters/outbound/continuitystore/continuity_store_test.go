package continuitystore

import (
	"context"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestLocalResponseContinuityStore_MaterializesLinkedThread(t *testing.T) {
	baseTime := time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	now := baseTime
	store := NewLocalResponseContinuityStore(LocalResponseContinuityStoreConfig{
		Now: func() time.Time { return now },
	})

	namespace := canonical.NewContinuationNamespace("alpha")
	parent := canonical.NewContinuitySnapshot("resp_parent", "m", []canonical.CanonicalItem{
		canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		canonical.NewTextItem(canonical.ItemAuthorAssistant, "hello"),
	})
	if err := store.Store(context.Background(), namespace, parent); err != nil {
		t.Fatalf("Store(parent) returned error: %v", err)
	}

	now = now.Add(10 * time.Minute)
	child := canonical.NewContinuitySnapshot("resp_child", "m", []canonical.CanonicalItem{
		canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		canonical.NewTextItem(canonical.ItemAuthorAssistant, "hello"),
		canonical.NewTextItem(canonical.ItemAuthorUser, "continue"),
		canonical.NewTextItem(canonical.ItemAuthorAssistant, "done"),
	})
	if err := store.Store(context.Background(), namespace, child); err != nil {
		t.Fatalf("Store(child) returned error: %v", err)
	}

	got, ok, err := store.Load(context.Background(), "resp_child")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !ok {
		t.Fatal("Load returned ok=false, want true")
	}
	if len(got.Thread) != 4 {
		t.Fatalf("thread len = %d, want 4", len(got.Thread))
	}
	if got.Thread[3].Text != "done" {
		t.Fatalf("last item text = %q, want %q", got.Thread[3].Text, "done")
	}
}

func TestLocalResponseContinuityStore_MatchPrefixChoosesLatestEquivalentCandidate(t *testing.T) {
	baseTime := time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	now := baseTime
	store := NewLocalResponseContinuityStore(LocalResponseContinuityStoreConfig{
		Now: func() time.Time { return now },
	})

	namespace := canonical.NewContinuationNamespace("alpha")
	thread := []canonical.CanonicalItem{
		canonical.NewTextItem(canonical.ItemAuthorUser, "shared"),
	}
	if err := store.Store(context.Background(), namespace, canonical.NewContinuitySnapshot("resp_old", "m", thread)); err != nil {
		t.Fatalf("Store(old) returned error: %v", err)
	}

	now = now.Add(30 * time.Minute)
	if err := store.Store(context.Background(), namespace, canonical.NewContinuitySnapshot("resp_new", "m", thread)); err != nil {
		t.Fatalf("Store(new) returned error: %v", err)
	}

	match, ok, err := store.MatchPrefix(context.Background(), namespace, []canonical.CanonicalItem{
		canonical.NewTextItem(canonical.ItemAuthorUser, "shared"),
		canonical.NewTextItem(canonical.ItemAuthorAssistant, "branch"),
	})
	if err != nil {
		t.Fatalf("MatchPrefix returned error: %v", err)
	}
	if !ok {
		t.Fatal("MatchPrefix returned ok=false, want true")
	}
	if match.Snapshot.ResponseID != "resp_new" {
		t.Fatalf("matched response id = %q, want %q", match.Snapshot.ResponseID, "resp_new")
	}
	if match.PrefixLength != 1 {
		t.Fatalf("prefix len = %d, want 1", match.PrefixLength)
	}
}

func TestLocalResponseContinuityStore_EvictsExpiredRecentWindow(t *testing.T) {
	baseTime := time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	now := baseTime
	store := NewLocalResponseContinuityStore(LocalResponseContinuityStoreConfig{
		Now: func() time.Time { return now },
	})

	namespace := canonical.NewContinuationNamespace("alpha")
	if err := store.Store(context.Background(), namespace, canonical.NewContinuitySnapshot("resp_1", "m", []canonical.CanonicalItem{
		canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
	})); err != nil {
		t.Fatalf("Store returned error: %v", err)
	}

	now = now.Add(5 * time.Hour)
	_, ok, err := store.Load(context.Background(), "resp_1")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if ok {
		t.Fatal("Load returned ok=true, want false after retention expiry")
	}
}

func TestLocalResponseContinuityStore_TouchKeepsActiveAncestorsAlive(t *testing.T) {
	baseTime := time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	now := baseTime
	store := NewLocalResponseContinuityStore(LocalResponseContinuityStoreConfig{
		Now: func() time.Time { return now },
	})

	namespace := canonical.NewContinuationNamespace("alpha")
	if err := store.Store(context.Background(), namespace, canonical.NewContinuitySnapshot("resp_1", "m", []canonical.CanonicalItem{
		canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
	})); err != nil {
		t.Fatalf("Store(resp_1) returned error: %v", err)
	}

	now = now.Add(2 * time.Hour)
	if err := store.Store(context.Background(), namespace, canonical.NewContinuitySnapshot("resp_2", "m", []canonical.CanonicalItem{
		canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		canonical.NewTextItem(canonical.ItemAuthorAssistant, "hello"),
	})); err != nil {
		t.Fatalf("Store(resp_2) returned error: %v", err)
	}

	now = now.Add(3 * time.Hour)
	got, ok, err := store.Load(context.Background(), "resp_2")
	if err != nil {
		t.Fatalf("Load(resp_2) returned error: %v", err)
	}
	if !ok {
		t.Fatal("Load(resp_2) returned ok=false, want true")
	}
	if len(got.Thread) != 2 {
		t.Fatalf("thread len = %d, want 2", len(got.Thread))
	}
}
