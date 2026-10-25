package connections

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCheckedAuditAtomicPublicationReplayAndRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	store, err := openInlineStoreFixture(t, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := fixtureConnection(t).Spec()
	admit := func(context.Context) error { return nil }
	original, _, err := store.CreateChecked(t.Context(), "PRIVATE_LABEL", spec, "local-operator", "PRIVATE_RETRY_KEY", admit)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Audits()) != 1 {
		t.Fatal("missing create fact")
	}
	if _, replayed, err := store.CreateChecked(t.Context(), "PRIVATE_LABEL", spec, "local-operator", "PRIVATE_RETRY_KEY", admit); err != nil || !replayed || len(store.Audits()) != 1 {
		t.Fatal("replay duplicated audit")
	}
	candidate, err := original.EditSameIdentity(original.Revision(), "PRIVATE_NEW_LABEL", spec, original.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	replace := store.publishSection
	store.publishSection = func([]byte) error { return errors.New("injected failure") }
	if err := store.PublishChecked(t.Context(), candidate, original.Revision(), "local-operator", admit); err == nil {
		t.Fatal("failed publication accepted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || len(store.Audits()) != 1 {
		t.Fatal("failed publication changed audit")
	}
	store.publishSection = replace
	if err := store.PublishChecked(t.Context(), candidate, original.Revision(), "local-operator", admit); err != nil {
		t.Fatal(err)
	}
	if err := store.PublishChecked(t.Context(), candidate, original.Revision(), "local-operator", admit); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	audits := store.Audits()
	if len(audits) != 2 || audits[0].Action != "connection.create" || audits[1].Action != "connection.edit" || audits[1].BeforeRevision != "1" || audits[1].AfterRevision != "2" || audits[1].Caller != "local-operator" || audits[1].ConnectionID != original.ID().String() {
		t.Fatalf("incorrect facts: %+v", audits)
	}
	audits[0].Caller = "mutated"
	if store.Audits()[0].Caller != "local-operator" {
		t.Fatal("audit read aliases store")
	}
	raw, _ := json.Marshal(audits)
	for _, forbidden := range []string{"PRIVATE_LABEL", "PRIVATE_NEW_LABEL", "PRIVATE_RETRY_KEY", "example.com", "MCP_TOKEN", "credential_ref"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("audit disclosed %s", forbidden)
		}
	}
	// Model retry expiry and resource deletion without requiring current resource
	// existence to validate history on restart. Runtime deletion is separately owned.
	if err := store.publishDocument(map[connectionidentity.ID]Connection{}, map[string]creationReceipt{}, store.audits); err != nil {
		t.Fatal(err)
	}
	reopened, err := openInlineStoreFixture(t, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.List()) != 0 || len(reopened.Audits()) != 2 {
		t.Fatal("audit depended on current config/retry state")
	}
}

func TestAuditRejectsCorruptFactsAndUncheckedPublicationAddsNone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	store, _ := openInlineStoreFixture(t, path, nil)
	if err := store.Publish(fixtureConnection(t), Revision{}); err != nil {
		t.Fatal(err)
	}
	if len(store.Audits()) != 0 {
		t.Fatal("unchecked publication fabricated attribution")
	}
	for _, fact := range []string{
		`{"action":"unknown","caller":"local","connection_id":"cn_a","after_revision":"1","committed_at":"2026-10-16T12:00:00Z"}`,
		`{"action":"connection.create","caller":"","connection_id":"cn_a","after_revision":"1","committed_at":"2026-10-16T12:00:00Z"}`,
		`{"action":"connection.edit","caller":"local","connection_id":"cn_a","before_revision":"1","after_revision":"3","committed_at":"2026-10-16T12:00:00Z"}`,
		`{"action":"connection.create","caller":"local","connection_id":"cn_a","after_revision":"1"}`,
		`{"action":"connection.create","caller":"local","connection_id":"cn_a","after_revision":"1","committed_at":"2026-10-16T12:00:00Z","credential_ref":"env:PRIVATE"}`,
	} {
		if _, err := OpenInline([]byte(`{"schema_version":1,"connections":[],"audits":[`+fact+`]}`), func([]byte) error { return nil }, nil); err == nil {
			t.Fatalf("admitted corrupt audit: %s", fact)
		}
	}
}

func TestCheckedDeletionAuditAdmissionReferencesAndAtomicFailure(t *testing.T) {
	lifecycle, registry, workspaces, value, attached := lifecycleFixture(t)
	admit := func(context.Context) error { return nil }
	denied := errors.New("denied")
	if err := lifecycle.DeleteChecked(t.Context(), value.ID(), value.Revision(), "operator", func(context.Context) error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("admission: %v", err)
	}
	if _, err := workspaces.Update(t.Context(), func(routing.Config) (routing.Config, error) { return attached, nil }); err != nil {
		t.Fatal(err)
	}
	var inUse *InUseError
	if err := lifecycle.DeleteChecked(t.Context(), value.ID(), value.Revision(), "operator", admit); !errors.As(err, &inUse) {
		t.Fatalf("reference refusal: %v", err)
	}
	if len(registry.Audits()) != 0 {
		t.Fatal("rejected deletion retained fact")
	}
	if _, err := workspaces.Update(t.Context(), func(routing.Config) (routing.Config, error) { return routing.Config{}, nil }); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(registry.path)
	replace := registry.publishSection
	registry.publishSection = func([]byte) error { return errors.New("injected write failure") }
	if err := lifecycle.DeleteChecked(t.Context(), value.ID(), value.Revision(), "operator", admit); err == nil {
		t.Fatal("failed deletion accepted")
	}
	after, _ := os.ReadFile(registry.path)
	if string(before) != string(after) || len(registry.Audits()) != 0 {
		t.Fatal("failed deletion changed facts")
	}
	registry.publishSection = replace
	if err := lifecycle.DeleteChecked(t.Context(), value.ID(), value.Revision(), "operator", admit); err != nil {
		t.Fatal(err)
	}
	reopened, err := openInlineStoreFixture(t, registry.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	audits := reopened.Audits()
	if len(reopened.List()) != 0 || len(audits) != 1 || audits[0].Action != "connection.delete" || audits[0].BeforeRevision != "1" || audits[0].AfterRevision != "" || audits[0].Caller != "operator" {
		t.Fatalf("deletion fact: %+v", audits)
	}
}
