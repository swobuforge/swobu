package accessstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckedPermissionResultIsDetachedAndReplayReportsCurrentRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	identity, _, err := store.Create("result", workspace)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PermissionRevision(identity.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	result, err := store.ApplyPermissionChecked(t.Context(), identity.ID, workspace, revision, "result-key", true, func() error { return nil }, deadline)
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.Permission == nil || !result.Permission.ExpiresAt.Equal(deadline) || result.Revision == revision {
		t.Fatal("apply omitted revised relationship")
	}
	result.Permission.ExpiresAt = time.Time{}
	if !store.List()[0].Permissions[0].ExpiresAt.Equal(deadline) {
		t.Fatal("result mutated retained authority")
	}
	if err := store.RevokePermission(identity.ID, workspace); err != nil {
		t.Fatal(err)
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err = reopened.ApplyPermissionChecked(t.Context(), identity.ID, workspace, revision, "result-key", true, func() error { return nil }, deadline)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reopened.PermissionRevision(identity.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Replayed || result.Permission != nil || result.Allowed || result.Revision != current {
		t.Fatal("replay misrepresented revoked current authority")
	}
}

func TestFailedCheckedPermissionPublicationReturnsNoSuccessSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	identity, _, err := store.Create("failed-result", workspace)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PermissionRevision(identity.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "preserved.json")
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := store.ApplyPermissionChecked(t.Context(), identity.ID, workspace, revision, "failed-result-key", false, func() error { return nil })
	if err == nil || result.Revision != "" || result.Permission != nil || result.Allowed || result.Replayed {
		t.Fatal("failed publication returned success snapshot")
	}
	if len(store.permissionReceipts) != 0 {
		t.Fatal("failed publication reserved receipt")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	result, err = store.ApplyPermissionChecked(t.Context(), identity.ID, workspace, revision, "failed-result-key", false, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.Permission != nil || result.Allowed || result.Revision == revision {
		t.Fatal("repaired apply lost fresh revised result")
	}
}
