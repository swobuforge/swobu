package accessstate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckedPermissionAdmissionRunsUnderLockBeforeApplyAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	identity, _, err := store.Create("admission", workspace)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PermissionRevision(identity.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	beforeRevision := store.Revision()
	if _, err := store.ApplyPermissionChecked(t.Context(), identity.ID, workspace, "", "missing-consent", false, func() error { return nil }); err == nil {
		t.Fatal("checked apply accepted missing relationship consent")
	}
	denied := errors.New("Node authority withdrawn")
	checks := 0
	check := func() error {
		checks++
		if store.mu.TryLock() {
			store.mu.Unlock()
			t.Fatal("admission ran outside transaction lock")
		}
		return denied
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyPermissionChecked(t.Context(), identity.ID, workspace, revision, "admission-key", false, check); !errors.Is(err, denied) {
		t.Fatalf("fresh denial=%v", err)
	}
	if len(store.permissionReceipts) != 0 || store.Revision() != beforeRevision {
		t.Fatal("denial published authority/receipt")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("denial changed persistent bytes")
	}
	allow := func() error {
		if store.mu.TryLock() {
			store.mu.Unlock()
			t.Fatal("allow ran outside transaction lock")
		}
		return nil
	}
	if _, err := store.ApplyPermissionChecked(t.Context(), identity.ID, workspace, revision, "admission-key", false, allow); err != nil {
		t.Fatal(err)
	}
	store, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterRevision := store.Revision()
	if _, err := store.ApplyPermissionChecked(t.Context(), identity.ID, workspace, revision, "admission-key", false, check); !errors.Is(err, denied) {
		t.Fatalf("replay denial=%v", err)
	}
	if _, err := store.ApplyPermissionChecked(t.Context(), identity.ID, workspace, revision, "admission-key", true, check); !errors.Is(err, denied) {
		t.Fatalf("authority must precede receipt conflict=%v", err)
	}
	after, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || store.Revision() != afterRevision || len(store.permissionReceipts) != 1 || checks != 3 {
		t.Fatal("denied replay changed retained state")
	}
	if _, err := store.ApplyPermissionChecked(t.Context(), identity.ID, workspace, revision, "admission-key", false, allow); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyPermissionChecked(t.Context(), identity.ID, workspace, afterRevision, "new-key", true, nil); err == nil {
		t.Fatal("nil admission accepted")
	}
}
