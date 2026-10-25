package accessstate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckedPermissionReceiptPersistsAdmittedLocalCallerNamespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	principal, _, err := store.Create("fixture", workspace)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PermissionRevision(principal.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyPermissionChecked(t.Context(), principal.ID, workspace, revision, "caller-key", false, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk struct {
		Receipts []struct {
			Caller  string `json:"caller"`
			KeyHash string `json:"key_hash"`
		} `json:"permission_receipts"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	unscoped := sha256.Sum256([]byte("caller-key"))
	if len(disk.Receipts) != 1 || disk.Receipts[0].Caller != "local" || disk.Receipts[0].KeyHash != hex.EncodeToString(unscoped[:]) {
		t.Fatal("receipt omitted current caller or corrupt key digest")
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reopened.ApplyPermissionChecked(t.Context(), principal.ID, workspace, revision, "caller-key", false, func() error { return nil })
	if err != nil || !result.Replayed {
		t.Fatalf("scoped reopen replay: %v %+v", err, result)
	}
}

func TestPermissionReceiptRejectsInvalidCallerMetadata(t *testing.T) {
	digest := sha256.Sum256([]byte("fixture"))
	for _, caller := range []string{"share", "acc_invalid", "session-from-payload"} {
		receipt := PermissionReceipt{Caller: caller, KeyHash: hex.EncodeToString(digest[:]), IntentHash: hex.EncodeToString(digest[:])}
		if err := validatePermissionReceipts([]PermissionReceipt{receipt}); err == nil {
			t.Fatalf("invalid caller accepted: %s", caller)
		}
	}
}

func TestScopedPermissionPublicationFailureReservesNoKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	principal, _, err := store.Create("fixture", workspace)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PermissionRevision(principal.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "access.before.json")
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyPermissionChecked(t.Context(), principal.ID, workspace, revision, "repair-key", false, func() error { return nil }); err == nil {
		t.Fatal("blocked publication succeeded")
	}
	if len(store.permissionReceipts) != 0 {
		t.Fatal("failed publication reserved caller key")
	}
	current, err := store.PermissionRevision(principal.ID, workspace)
	if err != nil || current != revision {
		t.Fatal("failed publication changed consent")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	result, err := store.ApplyPermissionChecked(t.Context(), principal.ID, workspace, revision, "repair-key", false, func() error { return nil })
	if err != nil || result.Replayed {
		t.Fatalf("repaired publication failed or falsely replayed: %v", err)
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err = reopened.ApplyPermissionChecked(t.Context(), principal.ID, workspace, revision, "repair-key", false, func() error { return nil })
	if err != nil || !result.Replayed {
		t.Fatalf("reopened scoped receipt missing: %v", err)
	}
}
