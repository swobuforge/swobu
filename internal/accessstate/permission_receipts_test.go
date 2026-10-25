package accessstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPermissionReceiptCommitsWithAuthorityAndReplayNeverRegrants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := store.Create("operator", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	revision := store.Revision()
	if err := store.ApplyPermission(identity.ID, workspace, revision, "reviewed-key", true); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokePermission(identity.ID, workspace); err != nil {
		t.Fatal(err)
	}
	store, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	before := store.Revision()
	if err := store.ApplyPermission(identity.ID, workspace, revision, "reviewed-key", true); err != nil {
		t.Fatal(err)
	}
	if store.Revision() != before || hasPermission(store.List()[0], workspace) {
		t.Fatal("replay restored revoked authority")
	}
	if err := store.ApplyPermission(identity.ID, workspace, revision, "reviewed-key", false); !errors.Is(err, ErrPermissionIdempotencyConflict) {
		t.Fatalf("changed intent=%v", err)
	}
}

func TestPermissionReceiptSchemaRejectsMalformedAndDuplicateRecords(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for name, receipts := range map[string][]PermissionReceipt{
		"short":     {{Caller: "local", KeyHash: "a", IntentHash: digest}},
		"nonhex":    {{Caller: "local", KeyHash: digest, IntentHash: strings.Repeat("z", 64)}},
		"uppercase": {{Caller: "local", KeyHash: strings.Repeat("A", 64), IntentHash: digest}},
		"duplicate": {{Caller: "local", KeyHash: digest, IntentHash: digest}, {Caller: "local", KeyHash: digest, IntentHash: digest}},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(diskState{SchemaVersion: SchemaVersion, Access: []diskAccess{}, PermissionReceipts: receipts})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decode(raw); err == nil {
				t.Fatal("strict codec accepted malformed receipt")
			}
		})
	}
}

func TestNoOpPermissionReceiptPreservesRevisionAndSurvivesOtherWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "production")
	identity, _, err := store.Create("operator", workspace)
	if err != nil {
		t.Fatal(err)
	}
	revision := store.Revision()
	if err := store.ApplyPermission(identity.ID, workspace, revision, "no-op-key", true); err != nil {
		t.Fatal(err)
	}
	if store.Revision() != revision {
		t.Fatal("receipt metadata advanced semantic revision")
	}
	if err := store.GrantPermission(identity.ID, testWorkspace(t, "research")); err != nil {
		t.Fatal(err)
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.permissionReceipts) != 1 {
		t.Fatal("ordinary write discarded receipt")
	}
	before := reopened.Revision()
	if err := reopened.ApplyPermission(identity.ID, workspace, revision, "no-op-key", true); err != nil {
		t.Fatal(err)
	}
	if reopened.Revision() != before {
		t.Fatal("replay changed authority")
	}
}

func TestConcurrentPermissionRetryPublishesOneReceipt(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := store.Create("operator", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	revision := store.Revision()
	start := make(chan struct{})
	results := make(chan error, 8)
	for range 8 {
		go func() {
			<-start
			results <- store.ApplyPermission(identity.ID, workspace, revision, "shared-retry", true)
		}()
	}
	close(start)
	for range 8 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if len(store.permissionReceipts) != 1 {
		t.Fatalf("receipt count=%d", len(store.permissionReceipts))
	}
	if got := store.List()[0].Permissions; len(got) != 2 {
		t.Fatalf("permission count=%d", len(got))
	}
	before := store.Revision()
	if err := store.ApplyPermission(identity.ID, testWorkspace(t, "extra"), revision, "shared-retry", true); !errors.Is(err, ErrPermissionIdempotencyConflict) {
		t.Fatalf("changed resource=%v", err)
	}
	if store.Revision() != before {
		t.Fatal("changed resource applied")
	}
}

func TestPermissionPublicationFailureRecordsNeitherAuthorityNorReceipt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := store.Create("operator", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	revision := store.Revision()
	workspace := testWorkspace(t, "research")
	// A directory at the validated replacement target forces rename failure.
	backup := filepath.Join(dir, "preserved.json")
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyPermission(identity.ID, workspace, revision, "retry-key", true); err == nil {
		t.Fatal("failed publication succeeded")
	}
	if store.Revision() != revision || len(store.permissionReceipts) != 0 {
		t.Fatal("failed write changed memory or receipt")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if hasPermission(reopened.List()[0], workspace) || len(reopened.permissionReceipts) != 0 {
		t.Fatal("failed write published authority or receipt")
	}
	if err := reopened.ApplyPermission(identity.ID, workspace, revision, "retry-key", true); err != nil {
		t.Fatal(err)
	}
}
