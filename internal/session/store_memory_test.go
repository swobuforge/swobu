package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

func TestMemoryStoreFindsUniqueHistoryAndRejectsAmbiguousHistoryWithinWorkspace(t *testing.T) {
	store := newMemoryStore()
	fingerprint := testCheckpointFingerprint(t, "one")
	first := storeRecord("resp_first")
	first.HistoryFingerprint = &fingerprint
	if err := store.Put(context.Background(), "alpha", first); err != nil {
		t.Fatal(err)
	}
	match, err := store.FindByHistory(context.Background(), "alpha", fingerprint)
	got, found := match.Unique()
	if err != nil || !found || got.Response.Response().SwobuID != "resp_first" {
		t.Fatalf("first lookup = (%q, %t, %v)", got.Response.Response().SwobuID, found, err)
	}
	match, err = store.FindByHistory(context.Background(), "beta", fingerprint)
	if err != nil || !match.IsMissing() {
		t.Fatalf("cross-workspace lookup = (%#v, %v), want miss", match, err)
	}
	match, err = newMemoryStore().FindByHistory(context.Background(), "alpha", fingerprint)
	if err != nil || !match.IsMissing() {
		t.Fatalf("cross-store lookup = (%#v, %v), want miss", match, err)
	}

	second := storeRecord("resp_second")
	second.HistoryFingerprint = &fingerprint
	if err := store.Put(context.Background(), "alpha", second); err != nil {
		t.Fatal(err)
	}
	match, err = store.FindByHistory(context.Background(), "alpha", fingerprint)
	if err != nil || !match.IsAmbiguous() {
		t.Fatalf("two indistinguishable checkpoints lookup = (%#v, %v), want ambiguous", match, err)
	}
}

func TestMemoryStoreExpiryRemovesFingerprintIndexWithoutRemovingReplacement(t *testing.T) {
	store := newMemoryStore()
	current := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return current }
	fingerprint := testCheckpointFingerprint(t, "shared")
	oldExpiry := current.Add(time.Minute)
	old := storeRecord("resp_old")
	old.HistoryFingerprint, old.ExpiresAt = &fingerprint, &oldExpiry
	if err := store.Put(context.Background(), "alpha", old); err != nil {
		t.Fatal(err)
	}
	newExpiry := current.Add(time.Hour)
	newer := storeRecord("resp_new")
	newer.HistoryFingerprint, newer.ExpiresAt = &fingerprint, &newExpiry
	if err := store.Put(context.Background(), "alpha", newer); err != nil {
		t.Fatal(err)
	}
	current = current.Add(2 * time.Minute)
	match, err := store.FindByHistory(context.Background(), "alpha", fingerprint)
	got, found := match.Unique()
	if err != nil || !found || got.Response.Response().SwobuID != "resp_new" {
		t.Fatalf("lookup after old expiry = (%q, %t, %v)", got.Response.Response().SwobuID, found, err)
	}
	current = current.Add(time.Hour)
	match, err = store.FindByHistory(context.Background(), "alpha", fingerprint)
	if err != nil || !match.IsMissing() {
		t.Fatalf("lookup after replacement expiry = (%#v, %v), want miss", match, err)
	}
}

func testCheckpointFingerprint(t *testing.T, material string) historyfingerprint.History {
	t.Helper()
	request, err := historyfingerprint.FingerprintRequest("responses", []byte("request:"+material))
	if err != nil {
		t.Fatal(err)
	}
	response, err := historyfingerprint.FingerprintResponse("responses", []byte("response:"+material))
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := historyfingerprint.Advance(nil, request, response)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func TestMemoryStoreConcurrentAccess(t *testing.T) {
	store := NewMemoryStore()
	scope := "alpha"

	const goroutines = 32
	const iterations = 16

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

func TestMemoryStoreEvictsOldestExpiringRecordAtCapacity(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	for index := 0; index <= maxMemoryStoreRecords; index++ {
		id := canonical.SwobuResponseID(fmt.Sprintf("resp_%04d", index))
		record := storeRecord(id)
		expiresAt := now.Add(time.Duration(index+1) * time.Minute)
		record.ExpiresAt = &expiresAt
		if err := store.Put(context.Background(), "alpha", record); err != nil {
			t.Fatalf("Put(%s): %v", id, err)
		}
	}
	if len(store.records) != maxMemoryStoreRecords {
		t.Fatalf("record count = %d, want %d", len(store.records), maxMemoryStoreRecords)
	}
	if _, found, err := store.Get(context.Background(), "alpha", "resp_0000"); err != nil || found {
		t.Fatalf("oldest record lookup = (found %t, err %v), want eviction", found, err)
	}
	newestID := canonical.SwobuResponseID(fmt.Sprintf("resp_%04d", maxMemoryStoreRecords))
	if _, found, err := store.Get(context.Background(), "alpha", newestID); err != nil || !found {
		t.Fatalf("newest record lookup = (found %t, err %v), want retained", found, err)
	}
}

func TestMemoryStoreDefensivelyCopiesResponsesContinuation(t *testing.T) {
	store := NewMemoryStore()
	scope := "alpha"
	target := testBackendTarget(t, "m")
	responses := nativeResponses(target, "provider_resp_1")
	record := checkpoint("resp_1", makeRequest("m", makeItems("hello"), nil), makeResponse(mustMessageItem(canonical.MessageRoleAssistant, "ok")), responses)

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
	record := checkpoint("resp_expiry", makeRequest("m", makeItems("hello"), nil), makeResponse(mustMessageItem(canonical.MessageRoleAssistant, "ok")), nil)
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
	record := checkpoint("resp_expired", makeRequest("m", makeItems("hello"), nil), makeResponse(mustMessageItem(canonical.MessageRoleAssistant, "ok")), nil)
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
	} else if !errors.Is(err, ErrCheckpointExists) {
		t.Fatalf("duplicate Put error = %v, want ErrCheckpointExists", err)
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
	if got := len(store.records); got != maxMemoryStoreRecords {
		t.Fatalf("records before expiry = %d, want bounded count %d", got, maxMemoryStoreRecords)
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

func TestMemoryStorePartitionsCheckpointsByWorkspaceSlug(t *testing.T) {
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
		t.Fatalf("same checkpoint ID must be legal in another workspace: %v", err)
	}
	for _, workspaceSlug := range []string{"alpha", "beta"} {
		if got, found, err := store.Get(context.Background(), workspaceSlug, record.Response.Response().SwobuID); err != nil {
			t.Fatalf("Get %s: %v", workspaceSlug, err)
		} else if !found || got.Response.Response().SwobuID != record.Response.Response().SwobuID {
			t.Fatalf("Get %s = (%q, %t), want (%q, true)", workspaceSlug, got.Response.Response().SwobuID, found, record.Response.Response().SwobuID)
		}
	}
}

func storeRecord(id canonical.SwobuResponseID) Checkpoint {
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID(id.String())}, "test-model", nil, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		panic(err)
	}
	return Checkpoint{Response: response, CreatedAt: time.Now().UTC()}
}
