package connections

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

func TestCheckedReplacementAtomicFailureReplayAuditAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	store, _ := openInlineStoreFixture(t, path, nil)
	// Use a current timestamp so revision timestamps represent this command order.
	fixture := fixtureConnection(t)
	original, err := New(fixture.ID(), fixture.DisplayName(), fixture.Spec(), time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(original, Revision{}); err != nil {
		t.Fatal(err)
	}
	old := credentialref.Parse("env:MCP_TOKEN")
	next := credentialref.Parse("secretfile:unique-slot")
	admit := func(context.Context) error { return nil }
	before, _ := os.ReadFile(path)
	replace := store.publishSection
	store.publishSection = func([]byte) error { return errors.New("failure") }
	if _, _, err := store.ReplaceCredentialChecked(t.Context(), original.ID(), original.Revision(), "", old, next, "local", "private-key", admit); err == nil {
		t.Fatal("failed write accepted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || len(store.replacements) != 0 || len(store.Audits()) != 0 {
		t.Fatal("failed replacement changed state")
	}
	store.publishSection = replace
	committed, replayed, err := store.ReplaceCredentialChecked(t.Context(), original.ID(), original.Revision(), "", old, next, "local", "private-key", admit)
	if err != nil || replayed || committed.Revision().String() != "2" {
		t.Fatalf("replace: %v", err)
	}
	edited, err := committed.EditSameIdentity(committed.Revision(), "Changed", committed.Spec(), committed.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(edited, committed.Revision()); err != nil {
		t.Fatal(err)
	}
	store, err = openInlineStoreFixture(t, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, replayed, err := store.ReplaceCredentialChecked(t.Context(), original.ID(), original.Revision(), "", old, next, "local", "private-key", admit)
	if err != nil || !replayed || got.Revision() != committed.Revision() || got.DisplayName() != committed.DisplayName() {
		t.Fatal("lost original receipt")
	}
	denied := errors.New("denied")
	if _, _, err := store.ReplaceCredentialChecked(t.Context(), original.ID(), original.Revision(), "", old, next, "local", "private-key", func(context.Context) error { return denied }); !errors.Is(err, denied) {
		t.Fatal("replay bypassed admission")
	}
	if _, _, err := store.ReplaceCredentialChecked(t.Context(), original.ID(), original.Revision(), "", old, credentialref.Parse("env:DIFFERENT"), "local", "private-key", admit); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("intent conflict: %v", err)
	}
	audits := store.Audits()
	if len(audits) != 1 || audits[0].Action != "connection.credential_replace" || audits[0].BeforeRevision != "1" || audits[0].AfterRevision != "2" {
		t.Fatalf("audit: %+v", audits)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "private-key") {
		t.Fatal("raw retry key persisted")
	}
}

func TestCheckedReplacementConcurrentRetryAndCancellation(t *testing.T) {
	store, _ := openInlineStoreFixture(t, filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	fixture := fixtureConnection(t)
	original, _ := New(fixture.ID(), fixture.DisplayName(), fixture.Spec(), time.Now().Add(-time.Minute))
	if err := store.Publish(original, Revision{}); err != nil {
		t.Fatal(err)
	}
	old := credentialref.Parse("env:MCP_TOKEN")
	next := credentialref.Parse("env:NEXT")
	ctx, cancel := context.WithCancel(t.Context())
	if _, _, err := store.ReplaceCredentialChecked(ctx, original.ID(), original.Revision(), "", old, next, "local", "retry", func(context.Context) error { cancel(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("ignored cancellation")
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, _, err := store.ReplaceCredentialChecked(t.Context(), original.ID(), original.Revision(), "", old, next, "local", "retry", func(context.Context) error { return nil })
			if err != nil || got.Revision().String() != "2" {
				t.Errorf("retry: %v", err)
			}
		}()
	}
	wg.Wait()
	if len(store.Audits()) != 1 || len(store.replacements) != 1 {
		t.Fatal("concurrent retry duplicated facts")
	}
}

func TestReplacementReceiptExpiryAndCorruptRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	store, _ := openInlineStoreFixture(t, path, nil)
	fixture := fixtureConnection(t)
	original, _ := New(fixture.ID(), fixture.DisplayName(), fixture.Spec(), time.Now().Add(-time.Minute))
	if err := store.Publish(original, Revision{}); err != nil {
		t.Fatal(err)
	}
	old := credentialref.Parse("env:MCP_TOKEN")
	next := credentialref.Parse("env:NEXT")
	admit := func(context.Context) error { return nil }
	if _, _, err := store.ReplaceCredentialChecked(t.Context(), original.ID(), original.Revision(), "", old, next, "local", "retry", admit); err != nil {
		t.Fatal(err)
	}
	// Expiry must not reapply an old reviewed revision or revive its authority.
	for key, receipt := range store.replacements {
		receipt.ExpiresAt = time.Now().Add(-time.Second)
		store.replacements[key] = receipt
	}
	if _, _, err := store.ReplaceCredentialChecked(t.Context(), original.ID(), original.Revision(), "", old, next, "local", "retry", admit); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expired replay bypassed revision: %v", err)
	}
	for _, raw := range []string{
		`{"schema_version":1,"connections":[],"replacements":{"bad":{"intent":"bad"}}}`,
		`{"schema_version":1,"connections":[],"replacements":{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":{"intent":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","expires_at":"2026-10-17T12:00:00Z","result":{}}}}`,
	} {
		if _, err := OpenInline([]byte(raw), func([]byte) error { return nil }, nil); err == nil {
			t.Fatal("corrupt replacement receipt admitted")
		}
	}
}

func TestReplacementReceiptCapacityEvictsOnlyAfterDurableCommit(t *testing.T) {
	store, _ := openInlineStoreFixture(t, filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	fixture := fixtureConnection(t)
	original, _ := New(fixture.ID(), fixture.DisplayName(), fixture.Spec(), time.Now().Add(-time.Minute))
	if err := store.Publish(original, Revision{}); err != nil {
		t.Fatal(err)
	}
	old := credentialref.Parse("env:MCP_TOKEN")
	next := credentialref.Parse("env:NEXT")
	admit := func(context.Context) error { return nil }
	committed, _, err := store.ReplaceCredentialChecked(t.Context(), original.ID(), original.Revision(), "", old, next, "local", "retry", admit)
	if err != nil {
		t.Fatal(err)
	}
	var template replacementReceipt
	for _, receipt := range store.replacements {
		template = receipt
	}
	store.replacements = make(map[string]replacementReceipt, creationReceiptCapacity)
	for i := range creationReceiptCapacity {
		store.replacements[fmt.Sprintf("%064x", i)] = template
	}
	replace := store.publishSection
	store.publishSection = func([]byte) error { return errors.New("failure") }
	last := credentialref.Parse("env:LAST")
	if _, _, err := store.ReplaceCredentialChecked(t.Context(), committed.ID(), committed.Revision(), "", next, last, "local", "second", admit); err == nil {
		t.Fatal("failed publication accepted")
	}
	oldest := fmt.Sprintf("%064x", 0)
	if _, exists := store.replacements[oldest]; !exists || len(store.replacements) != creationReceiptCapacity || len(store.Audits()) != 1 {
		t.Fatal("failed publication evicted receipt or changed audit")
	}
	store.publishSection = replace
	if _, _, err := store.ReplaceCredentialChecked(t.Context(), committed.ID(), committed.Revision(), "", next, last, "local", "second", admit); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.replacements[oldest]; exists || len(store.replacements) != creationReceiptCapacity || len(store.Audits()) != 2 {
		t.Fatal("capacity eviction did not commit")
	}
	if _, err := openInlineStoreFixture(t, store.path, nil); err != nil {
		t.Fatalf("cannot reopen capacity document: %v", err)
	}
}

func TestCheckedStdioReplacementSelectsOneEnvironmentBinding(t *testing.T) {
	store, _ := openInlineStoreFixture(t, filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	old := credentialref.Parse("env:OLD")
	other := credentialref.Parse("env:OTHER")
	next := credentialref.Parse("env:NEW")
	spec, err := NewMCPStdioSpec("server", []string{"--mode", "safe"}, map[string]credentialref.Ref{"TOKEN": old, "OTHER": other})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.New()
	original, err := New(id, "Local Server", spec, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(original, Revision{}); err != nil {
		t.Fatal(err)
	}
	admit := func(context.Context) error { return nil }
	if _, _, err := store.ReplaceCredentialChecked(t.Context(), id, original.Revision(), "MISSING", old, next, "local", "retry", admit); !errors.Is(err, ErrCredentialBinding) {
		t.Fatalf("invalid binding: %v", err)
	}
	committed, _, err := store.ReplaceCredentialChecked(t.Context(), id, original.Revision(), "TOKEN", old, next, "local", "retry", admit)
	if err != nil {
		t.Fatal(err)
	}
	result := committed.Spec().(MCPStdioSpec)
	if result.Environment()["TOKEN"] != next || result.Environment()["OTHER"] != other || result.Executable() != spec.Executable() || !reflect.DeepEqual(result.Args(), spec.Args()) {
		t.Fatal("replacement changed unrelated stdio configuration")
	}
}
