package connections

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
)

func TestCreationReplaySurvivesEditRestartAndReauthorizes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	store, err := openInlineStoreFixture(t, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := fixtureConnection(t).Spec()
	admit := func(context.Context) error { return nil }
	original, replayed, err := store.CreateChecked(t.Context(), "Original", spec, "operator", "private-retry-key", admit)
	if err != nil || replayed {
		t.Fatalf("create: %v %v", replayed, err)
	}
	candidate, err := original.EditSameIdentity(original.Revision(), "Changed", spec, original.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(candidate, original.Revision()); err != nil {
		t.Fatal(err)
	}
	reopened, err := openInlineStoreFixture(t, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, replayed, err := reopened.CreateChecked(t.Context(), "Original", spec, "operator", "private-retry-key", admit)
	if err != nil || !replayed || got.ID() != original.ID() || got.Revision() != original.Revision() || got.DisplayName() != "Original" {
		t.Fatalf("original receipt lost: %v %v %v", got, replayed, err)
	}
	denied := errors.New("admission denied")
	if _, _, err := reopened.CreateChecked(t.Context(), "Original", spec, "operator", "private-retry-key", func(context.Context) error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("replay bypassed admission: %v", err)
	}
	if _, _, err := reopened.CreateChecked(t.Context(), "Different", spec, "operator", "private-retry-key", admit); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("intent conflict: %v", err)
	}
	other, replayed, err := reopened.CreateChecked(t.Context(), "Original", spec, "other-operator", "private-retry-key", admit)
	if err != nil || replayed || other.ID() == original.ID() {
		t.Fatal("caller scope did not isolate retry keys")
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "private-retry-key") {
		t.Fatal("raw retry key persisted")
	}
}

func TestCreationFailureCancellationAndConcurrentRetries(t *testing.T) {
	store, err := openInlineStoreFixture(t, filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := fixtureConnection(t).Spec()
	admit := func(context.Context) error { return nil }
	replace := store.publishSection
	store.publishSection = func([]byte) error { return errors.New("injected failure") }
	if _, _, err := store.CreateChecked(t.Context(), "Account", spec, "operator", "retry", admit); err == nil {
		t.Fatal("failed write accepted")
	}
	if len(store.List()) != 0 || len(store.creations) != 0 || len(store.Audits()) != 0 {
		t.Fatal("failed write published configuration or receipt")
	}
	store.publishSection = replace
	ctx, cancel := context.WithCancel(t.Context())
	if _, _, err := store.CreateChecked(ctx, "Account", spec, "operator", "retry", func(context.Context) error { cancel(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("effect ignored cancellation: %v", err)
	}
	var wg sync.WaitGroup
	ids := make(chan connectionidentity.ID, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, _, err := store.CreateChecked(t.Context(), "Account", spec, "operator", "retry", admit)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- value.ID()
		}()
	}
	wg.Wait()
	close(ids)
	var first connectionidentity.ID
	for id := range ids {
		if first.String() == "" {
			first = id
		}
		if id != first {
			t.Fatal("retry minted multiple identities")
		}
	}
	if len(store.List()) != 1 || len(store.creations) != 1 {
		t.Fatal("retry duplicated durable state")
	}
}

func TestCreationReceiptExpiryAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	store, _ := openInlineStoreFixture(t, path, nil)
	spec := fixtureConnection(t).Spec()
	admit := func(context.Context) error { return nil }
	original, _, err := store.CreateChecked(t.Context(), "Account", spec, "operator", "retry", admit)
	if err != nil {
		t.Fatal(err)
	}
	for key, receipt := range store.creations {
		receipt.ExpiresAt = time.Now().Add(-time.Second)
		store.creations[key] = receipt
	}
	fresh, replayed, err := store.CreateChecked(t.Context(), "Account", spec, "operator", "retry", admit)
	if err != nil || replayed || fresh.ID() == original.ID() {
		t.Fatal("expired retry retained original result")
	}
	if _, err := OpenInline([]byte(`{"schema_version":1,"connections":[],"creations":{"bad":{"intent":"bad"}}}`), func([]byte) error { return nil }, nil); err == nil {
		t.Fatal("corrupt receipt admitted")
	}
}

func TestCreationReceiptCapacityEvictsOnlyOnSuccessfulPublication(t *testing.T) {
	store, err := openInlineStoreFixture(t, filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := fixtureConnection(t).Spec()
	admit := func(context.Context) error { return nil }
	_, _, err = store.CreateChecked(t.Context(), "Account", spec, "operator", "retry", admit)
	if err != nil {
		t.Fatal(err)
	}
	var template creationReceipt
	for _, receipt := range store.creations {
		template = receipt
	}
	store.creations = make(map[string]creationReceipt, creationReceiptCapacity)
	for i := range creationReceiptCapacity {
		store.creations[fmt.Sprintf("%064x", i)] = template
	}
	replace := store.publishSection
	store.publishSection = func([]byte) error { return errors.New("injected failure") }
	if _, _, err := store.CreateChecked(t.Context(), "Another", spec, "operator", "another", admit); err == nil {
		t.Fatal("failed write accepted")
	}
	oldest := fmt.Sprintf("%064x", 0)
	if _, exists := store.creations[oldest]; !exists || len(store.creations) != creationReceiptCapacity {
		t.Fatal("failed write evicted receipt")
	}
	store.publishSection = replace
	if _, _, err := store.CreateChecked(t.Context(), "Another", spec, "operator", "another", admit); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.creations[oldest]; exists || len(store.creations) != creationReceiptCapacity {
		t.Fatal("successful write did not evict deterministically")
	}
	if _, err := openInlineStoreFixture(t, store.path, nil); err != nil {
		t.Fatalf("capacity document cannot reopen: %v", err)
	}
}

func TestCheckedPublicationReauthorizesBeforeRevisionCommit(t *testing.T) {
	store, err := openInlineStoreFixture(t, filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	original := fixtureConnection(t)
	if err := store.Publish(original, Revision{}); err != nil {
		t.Fatal(err)
	}
	candidate, err := original.EditSameIdentity(original.Revision(), "Changed", original.Spec(), original.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(store.path)
	denied := errors.New("expired operator authority")
	if err := store.PublishChecked(t.Context(), candidate, original.Revision(), "operator", func(context.Context) error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("admission: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err := store.PublishChecked(ctx, candidate, original.Revision(), "operator", func(context.Context) error { cancel(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("effect ignored cancellation: %v", err)
	}
	after, _ := os.ReadFile(store.path)
	current, _ := store.Get(original.ID())
	if string(before) != string(after) || current.Revision() != original.Revision() {
		t.Fatal("denied/canceled edit committed")
	}
}
