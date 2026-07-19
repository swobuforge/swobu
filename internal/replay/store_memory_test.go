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
				id := canonical.SwobuResponseID(fmt.Sprintf("resp_%d_%d", g, i))
				record := storeRecord(id)
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
				if got.Response.Response().SwobuID != id {
					errCh <- fmt.Errorf("Get(%s) returned %s", id, got.Response.Response().SwobuID)
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

func TestMemoryStoreDefensivelyCopiesResponsesRefinement(t *testing.T) {
	store := NewMemoryStore()
	scope := "alpha"
	target := testBackendTarget(t, "m")
	responses := nativeResponses(target, "provider_resp_1")
	record := replayRecord("resp_1", makeRequest("m", makeItems("hello"), nil), makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "ok")), responses)

	if err := store.Put(context.Background(), scope, record); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	responses.ProviderResponseID = "mutated"
	got, ok, err := store.Get(context.Background(), scope, "resp_1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok {
		t.Fatal("expected record to be found")
	}
	if got.Response.Response().Responses == nil || got.Response.Response().Responses.ProviderResponseID != "provider_resp_1" {
		t.Fatalf("stored Responses refinement = %#v", got.Response.Response().Responses)
	}

	gotRef := got.Response.Response()
	gotRef.Responses.ProviderResponseID = "changed"
	gotAgain, ok, err := store.Get(context.Background(), scope, "resp_1")
	if err != nil {
		t.Fatalf("second Get failed: %v", err)
	}
	if !ok {
		t.Fatal("expected record to be found on second read")
	}
	if gotAgain.Response.Response().Responses == nil || gotAgain.Response.Response().Responses.ProviderResponseID != "provider_resp_1" {
		t.Fatalf("store was mutated through Get result: %#v", gotAgain.Response.Response().Responses)
	}
}

func TestMemoryStoreDefensivelyCopiesExpiresAt(t *testing.T) {
	store := NewMemoryStore()
	scope := "alpha"
	expiresAt := time.Now().UTC().Add(time.Hour)
	record := replayRecord("resp_expiry", makeRequest("m", makeItems("hello"), nil), makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "ok")), nil)
	record.ExpiresAt = &expiresAt
	record.CreatedAt = time.Now().UTC()

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
	record := replayRecord("resp_expired", makeRequest("m", makeItems("hello"), nil), makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "ok")), nil)
	record.ExpiresAt = &expiredAt
	record.CreatedAt = time.Now().UTC().Add(-2 * time.Minute)

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
	record := storeRecord("resp_1")
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
	if got.Response.Response().SwobuID != "resp_1" {
		t.Fatalf("stored record id = %q, want resp_1", got.Response.Response().SwobuID)
	}
}

func TestMemoryStoreWriteReclaimsHighVolumeExpiredRecordsWithoutReadingThem(t *testing.T) {
	store := newMemoryStore()
	current := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return current }
	expiresAt := current.Add(time.Minute)

	const recordCount = 10_000
	for i := 0; i < recordCount; i++ {
		record := storeRecord(canonical.SwobuResponseID(fmt.Sprintf("expired_%05d", i)))
		record.CreatedAt, record.ExpiresAt = current, &expiresAt
		if err := store.Put(context.Background(), "alpha", record); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	if got := len(store.records); got != recordCount {
		t.Fatalf("records before expiry = %d, want %d", got, recordCount)
	}

	current = current.Add(2 * time.Minute)
	live := storeRecord("live")
	live.CreatedAt = current
	if err := store.Put(context.Background(), "alpha", live); err != nil {
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
	record := storeRecord("resp_shared")

	if err := store.Put(context.Background(), "alpha", record); err != nil {
		t.Fatalf("Put alpha: %v", err)
	}
	if _, found, err := store.Get(context.Background(), "beta", record.Response.Response().SwobuID); err != nil {
		t.Fatalf("Get beta: %v", err)
	} else if found {
		t.Fatal("record from alpha was visible in beta")
	}
	if err := store.Put(context.Background(), "beta", record); err != nil {
		t.Fatalf("same replay ID must be legal in another workspace: %v", err)
	}
	for _, workspaceSlug := range []string{"alpha", "beta"} {
		if got, found, err := store.Get(context.Background(), workspaceSlug, record.Response.Response().SwobuID); err != nil {
			t.Fatalf("Get %s: %v", workspaceSlug, err)
		} else if !found || got.Response.Response().SwobuID != record.Response.Response().SwobuID {
			t.Fatalf("Get %s = (%q, %t), want (%q, true)", workspaceSlug, got.Response.Response().SwobuID, found, record.Response.Response().SwobuID)
		}
	}
}

func storeRecord(id canonical.SwobuResponseID) Record {
	return Record{Response: canonical.NewConversationOutput(canonical.NewSwobuResponseID(id.String()), "", nil, ""), CreatedAt: time.Now().UTC()}
}
