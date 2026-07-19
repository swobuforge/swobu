package replay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestMemoryStoreConcurrentAccess(t *testing.T) {
	store := NewMemoryStore()
	scope := "alpha"

	const goroutines = 32
	const iterations = 64

	start := make(chan struct{})
	errCh := make(chan error, goroutines*iterations)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				id := ID(fmt.Sprintf("resp_%d_%d", g, i))
				record := Record{
					ID:        id,
					CreatedAt: time.Now().UTC(),
				}
				if err := store.Put(context.Background(), scope, record); err != nil {
					errCh <- fmt.Errorf("Put(%s) error: %w", id, err)
					return
				}
				got, ok, err := store.Get(context.Background(), scope, id)
				if err != nil {
					errCh <- fmt.Errorf("Get(%s) error: %w", id, err)
					return
				}
				if !ok {
					errCh <- fmt.Errorf("Get(%s) missing record", id)
					return
				}
				if got.ID != id {
					errCh <- fmt.Errorf("Get(%s) returned %s", id, got.ID)
					return
				}
			}
		}(g)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestMemoryStoreDefensivelyCopiesNativeContinuation(t *testing.T) {
	store := NewMemoryStore()
	scope := "alpha"
	native := testBackendTarget(t, "m").NativeContinuation("provider_resp_1")
	record := Record{
		ID:        "resp_1",
		Request:   makeRequest("m", makeItems("hello"), canonical.TurnRef{}),
		Response:  makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "ok")),
		Native:    native,
		CreatedAt: time.Now().UTC(),
	}

	if err := store.Put(context.Background(), scope, record); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	native.ID = "mutated"
	got, ok, err := store.Get(context.Background(), scope, "resp_1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok {
		t.Fatal("expected record to be found")
	}
	if got.Native == nil {
		t.Fatal("expected native ref to be present")
	}
	if got.Native.ID != "provider_resp_1" {
		t.Fatalf("stored native ID = %q, want provider_resp_1", got.Native.ID)
	}

	got.Native.ID = "changed"
	gotAgain, ok, err := store.Get(context.Background(), scope, "resp_1")
	if err != nil {
		t.Fatalf("second Get failed: %v", err)
	}
	if !ok {
		t.Fatal("expected record to be found on second read")
	}
	if gotAgain.Native == nil {
		t.Fatal("expected native ref to be present on second read")
	}
	if gotAgain.Native.ID != "provider_resp_1" {
		t.Fatalf("store was mutated through Get result, native ID = %q", gotAgain.Native.ID)
	}
}

func TestMemoryStoreDefensivelyCopiesExpiresAt(t *testing.T) {
	store := NewMemoryStore()
	scope := "alpha"
	expiresAt := time.Now().UTC().Add(time.Hour)
	record := Record{
		ID:        "resp_expiry",
		Request:   makeRequest("m", makeItems("hello"), canonical.TurnRef{}),
		Response:  makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "ok")),
		ExpiresAt: &expiresAt,
		CreatedAt: time.Now().UTC(),
	}

	if err := store.Put(context.Background(), scope, record); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	expiresAt = time.Now().UTC()
	got, ok, err := store.Get(context.Background(), scope, "resp_expiry")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok {
		t.Fatal("expected record to be found")
	}
	if got.ExpiresAt == nil {
		t.Fatal("expected expiry to be present")
	}
	if !got.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("stored expiry = %v, want future time", got.ExpiresAt)
	}
	got.ExpiresAt = nil
	gotAgain, ok, err := store.Get(context.Background(), scope, "resp_expiry")
	if err != nil {
		t.Fatalf("second Get failed: %v", err)
	}
	if !ok {
		t.Fatal("expected record to be found on second read")
	}
	if gotAgain.ExpiresAt == nil {
		t.Fatal("expected expiry to remain present on second read")
	}
}

func TestMemoryStoreRejectsExpiredRecord(t *testing.T) {
	store := NewMemoryStore()
	scope := "alpha"
	expiredAt := time.Now().UTC().Add(-time.Minute)
	record := Record{
		ID:        "resp_expired",
		Request:   makeRequest("m", makeItems("hello"), canonical.TurnRef{}),
		Response:  makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "ok")),
		ExpiresAt: &expiredAt,
		CreatedAt: time.Now().UTC().Add(-2 * time.Minute),
	}

	if err := store.Put(context.Background(), scope, record); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, ok, err := store.Get(context.Background(), scope, "resp_expired")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if ok {
		t.Fatalf("expected expired record to be rejected, got %+v", got)
	}
}

func TestMemoryStoreRejectsDuplicateIDs(t *testing.T) {
	store := NewMemoryStore()
	scope := "alpha"
	record := Record{
		ID:        "resp_1",
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Put(context.Background(), scope, record); err != nil {
		t.Fatalf("first Put failed: %v", err)
	}
	if err := store.Put(context.Background(), scope, record); err == nil {
		t.Fatal("expected duplicate Put to fail")
	} else if !errors.Is(err, ErrReplayRecordExists) {
		t.Fatalf("duplicate Put error = %v, want ErrReplayRecordExists", err)
	}
	got, ok, err := store.Get(context.Background(), scope, "resp_1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok {
		t.Fatal("expected record to remain present after duplicate Put rejection")
	}
	if got.ID != "resp_1" {
		t.Fatalf("stored record id = %q, want resp_1", got.ID)
	}
}

func TestMemoryStoreWriteReclaimsHighVolumeExpiredRecordsWithoutReadingThem(t *testing.T) {
	store := newMemoryStore()
	current := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return current }
	expiresAt := current.Add(time.Minute)

	const recordCount = 10_000
	for i := 0; i < recordCount; i++ {
		record := Record{ID: ID(fmt.Sprintf("expired_%05d", i)), CreatedAt: current, ExpiresAt: &expiresAt}
		if err := store.Put(context.Background(), "alpha", record); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	if got := len(store.records); got != recordCount {
		t.Fatalf("records before expiry = %d, want %d", got, recordCount)
	}

	current = current.Add(2 * time.Minute)
	if err := store.Put(context.Background(), "alpha", Record{ID: "live", CreatedAt: current}); err != nil {
		t.Fatal(err)
	}
	if got := len(store.records); got != 1 {
		t.Fatalf("records after write reclamation = %d, want 1", got)
	}
	if got := store.expires.Len(); got != 1 {
		t.Fatalf("expiration index after reclamation = %d, want 1", got)
	}
}

func TestDaemonReplayStorePartitionsByWorkspaceSlug(t *testing.T) {
	store := NewMemoryStore()
	record := Record{ID: "resp_shared", CreatedAt: time.Now().UTC()}

	if err := store.Put(context.Background(), "alpha", record); err != nil {
		t.Fatalf("Put alpha: %v", err)
	}
	if _, found, err := store.Get(context.Background(), "beta", record.ID); err != nil {
		t.Fatalf("Get beta: %v", err)
	} else if found {
		t.Fatal("record from alpha was visible in beta")
	}
	if err := store.Put(context.Background(), "beta", record); err != nil {
		t.Fatalf("same replay ID must be legal in another workspace: %v", err)
	}
	for _, workspaceSlug := range []string{"alpha", "beta"} {
		if got, found, err := store.Get(context.Background(), workspaceSlug, record.ID); err != nil {
			t.Fatalf("Get %s: %v", workspaceSlug, err)
		} else if !found || got.ID != record.ID {
			t.Fatalf("Get %s = (%q, %t), want (%q, true)", workspaceSlug, got.ID, found, record.ID)
		}
	}
}
