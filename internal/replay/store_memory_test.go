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
	scope := Scope{Namespace: "alpha", CallerKey: "local"}

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
					Scope:     scope,
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

func TestMemoryStoreDefensivelyCopiesNativeRef(t *testing.T) {
	store := NewMemoryStore()
	scope := Scope{Namespace: "alpha", CallerKey: "local"}
	native := &NativeRef{
		ReplayID: "resp_1",
		Target:   testTarget(),
		Kind:     NativeRefProviderResponseID,
		Value:    "provider_resp_1",
	}
	record := Record{
		ID:        "resp_1",
		Scope:     scope,
		Request:   makeRequest("m", makeItems("hello"), canonical.TurnRef{}),
		Response:  makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "ok")),
		Native:    native,
		CreatedAt: time.Now().UTC(),
	}

	if err := store.Put(context.Background(), scope, record); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	native.Value = "mutated"
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
	if got.Native.Value != "provider_resp_1" {
		t.Fatalf("stored native value = %q, want provider_resp_1", got.Native.Value)
	}

	got.Native.Value = "changed"
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
	if gotAgain.Native.Value != "provider_resp_1" {
		t.Fatalf("store was mutated through Get result, native value = %q", gotAgain.Native.Value)
	}
}

func TestMemoryStoreDefensivelyCopiesExpiresAt(t *testing.T) {
	store := NewMemoryStore()
	scope := Scope{Namespace: "alpha", CallerKey: "local"}
	expiresAt := time.Now().UTC().Add(time.Hour)
	record := Record{
		ID:        "resp_expiry",
		Scope:     scope,
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
	scope := Scope{Namespace: "alpha", CallerKey: "local"}
	expiredAt := time.Now().UTC().Add(-time.Minute)
	record := Record{
		ID:        "resp_expired",
		Scope:     scope,
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
	scope := Scope{Namespace: "alpha", CallerKey: "local"}
	record := Record{
		ID:        "resp_1",
		Scope:     scope,
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

func TestMemoryStoreRejectsScopeMismatch(t *testing.T) {
	store := NewMemoryStore()
	recordScope := Scope{Namespace: "alpha", CallerKey: "local"}
	callScope := Scope{Namespace: "beta", CallerKey: "local"}
	record := Record{
		ID:        "resp_1",
		Scope:     recordScope,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Put(context.Background(), callScope, record); err == nil {
		t.Fatal("expected scope mismatch to fail")
	} else if err.Error() != "replay record scope mismatch" {
		t.Fatalf("scope mismatch error = %v, want replay record scope mismatch", err)
	}
	if _, ok, err := store.Get(context.Background(), recordScope, "resp_1"); err != nil {
		t.Fatalf("Get failed: %v", err)
	} else if ok {
		t.Fatal("expected scope-mismatched record not to be stored")
	}
}
