package accessstate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPermissionPreviewSharesValidationAndPreservesExactPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	identity, _, err := store.Create("preview", workspace)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PermissionRevision(identity.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	storeRevision := store.Revision()
	deadline := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, grant := range []bool{true, false} {
		expiry := time.Time{}
		if grant {
			expiry = deadline
		}
		impact, err := store.PreviewPermission(identity.ID, workspace, revision, grant, expiry)
		if err != nil {
			t.Fatal(err)
		}
		if impact.Revision != revision || impact.Before == nil || impact.CredentialCount != 1 {
			t.Fatal("preview lost authoritative impact")
		}
		if grant && (impact.After == nil || !impact.After.ExpiresAt.Equal(deadline)) {
			t.Fatal("proposed grant lost expiry")
		}
		if !grant && impact.After != nil {
			t.Fatal("revoke preview retained grant")
		}
		// Returned before/after values cannot mutate retained store authority.
		impact.Before.ExpiresAt = deadline
		if impact.After != nil {
			impact.After.ExpiresAt = deadline.Add(time.Hour)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || store.Revision() != storeRevision || len(store.permissionReceipts) != 0 {
		t.Fatal("preview published state or receipt")
	}
	if _, err := store.PreviewPermission(identity.ID, workspace, "stale", true, deadline); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale preview=%v", err)
	}
	if _, err := store.PreviewPermission(identity.ID, workspace, revision, false, deadline); err == nil {
		t.Fatal("revoke preview accepted grant-only expiry")
	}
	if _, err := store.ApplyPermissionChecked(t.Context(), identity.ID, workspace, revision, "reviewed", true, func() error { return nil }, deadline); err != nil {
		t.Fatal(err)
	}
	if !store.List()[0].Permissions[0].ExpiresAt.Equal(deadline) {
		t.Fatal("apply differs from preview candidate")
	}
	if _, err := store.PreviewPermission(identity.ID, workspace, revision, true, deadline); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("intervening edit preview=%v", err)
	}
}

func TestPermissionReadDetachedSnapshotAndExactExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	identity, _, err := store.Create("read", workspace)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := store.ApplyPermission(identity.ID, workspace, store.Revision(), "read-deadline", true, deadline); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return deadline.Add(-time.Nanosecond) }
	snapshot, err := store.ReadPermission(identity.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Allowed || snapshot.Permission == nil {
		t.Fatal("permission denied before deadline")
	}
	snapshot.Permission.ExpiresAt = time.Time{}
	store.now = func() time.Time { return deadline }
	snapshot, err = store.ReadPermission(identity.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Allowed || snapshot.Permission == nil || !snapshot.Permission.ExpiresAt.Equal(deadline) {
		t.Fatal("deadline/read mutation changed truth")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.PermissionRevision(identity.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || snapshot.Revision != current || len(store.permissionReceipts) != 1 {
		t.Fatal("read changed persistence or receipt")
	}
}
