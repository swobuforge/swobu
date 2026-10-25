package accessstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPermissionExpiryBoundaryRestartAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	identity, credential, err := store.Create("expiry", workspace)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Date(2026, 10, 31, 0, 0, 0, 123, time.UTC)
	store.now = func() time.Time { return deadline.Add(-time.Nanosecond) }
	revision := store.Revision()
	if err := store.ApplyPermission(identity.ID, workspace, revision, "expiry-key", true, deadline); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(credential.Token); err != nil {
		t.Fatal("permission denied before deadline")
	}
	if _, err := store.IssueCredential(identity.ID, workspace); err != nil {
		t.Fatal(err)
	}
	store, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return deadline }
	before := store.Revision()
	for _, check := range []func() error{
		func() error { _, err := store.Authenticate(credential.Token); return err },
		func() error { _, err := store.ResolveCredential(credential.Credential.ID); return err },
		func() error { _, err := store.IssueCredential(identity.ID, workspace); return err },
	} {
		if err := check(); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("exact expiry did not deny: %v", err)
		}
	}
	if store.Revision() != before {
		t.Fatal("time passage mutated durable revision")
	}
	if err := store.ApplyPermission(identity.ID, workspace, revision, "expiry-key", true, deadline); err != nil {
		t.Fatal(err)
	}
	if store.Revision() != before {
		t.Fatal("expired replay changed authority")
	}
	if err := store.ApplyPermission(identity.ID, workspace, revision, "expiry-key", true, deadline.Add(time.Hour)); !errors.Is(err, ErrPermissionIdempotencyConflict) {
		t.Fatalf("changed expiry replay=%v", err)
	}
	if err := store.ApplyPermission(identity.ID, workspace, before, "renewal", true, deadline.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(credential.Token); err != nil {
		t.Fatal("explicit renewal did not restore permission")
	}
	if err := store.RevokePermission(identity.ID, workspace); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyPermission(identity.ID, workspace, revision, "expiry-key", true, deadline); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(credential.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old replay restored revoked grant")
	}
}

func TestPermissionObjectCodecRejectsInvalidRelationships(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Create("codec", testWorkspace(t, "research")); err != nil {
		t.Fatal(err)
	}
	raw, err := encode(store.List())
	if err != nil {
		t.Fatal(err)
	}
	for _, permissions := range [][]json.RawMessage{
		{json.RawMessage(`{"workspace":"research","expires_at":"bad"}`)},
		{json.RawMessage(`{"workspace":"research","expires_at":"0001-01-01T00:00:00Z"}`)},
		{json.RawMessage(`{"workspace":"research","action":"manage"}`)},
		{json.RawMessage(`null`)},
		{json.RawMessage(`"research"`), json.RawMessage(`{"workspace":"research"}`)},
	} {
		var disk map[string]any
		if err := json.Unmarshal(raw, &disk); err != nil {
			t.Fatal(err)
		}
		access := disk["access"].([]any)[0].(map[string]any)
		access["permissions"] = permissions
		raw, err := json.Marshal(disk)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decode(raw); err == nil {
			t.Fatalf("accepted invalid permission %s", raw)
		}
	}
}

func TestFailedExpiryPublicationPreservesAuthorityAndRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	identity, credential, err := store.Create("failed-expiry", workspace)
	if err != nil {
		t.Fatal(err)
	}
	revision := store.Revision()
	backup := path + ".proof-backup"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	deadline := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := store.ApplyPermission(identity.ID, workspace, revision, "failed-expiry-key", true, deadline); err == nil {
		t.Fatal("obstructed publication succeeded")
	}
	if store.Revision() != revision || len(store.permissionReceipts) != 0 {
		t.Fatal("failed publication changed authority or receipts")
	}
	if _, err := store.Authenticate(credential.Token); err != nil {
		t.Fatal("failed edit expired live grant")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyPermission(identity.ID, workspace, revision, "failed-expiry-key", true, deadline); err != nil {
		t.Fatal(err)
	}
	store, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(credential.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("repaired expiry was not durable")
	}
	if len(store.permissionReceipts) != 1 {
		t.Fatal("repaired retry did not publish one receipt")
	}
}

func TestCurrentPermissionSchemaCannotDeriveAuthorityFromCredentials(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Create("schema", testWorkspace(t, "research")); err != nil {
		t.Fatal(err)
	}
	raw, err := encode(store.List())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []struct {
		name        string
		permissions any
		accepted    bool
	}{
		{"missing", nil, false},
		{"string", []string{"research"}, false},
		{"empty", []any{}, true},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			var disk map[string]any
			if err := json.Unmarshal(raw, &disk); err != nil {
				t.Fatal(err)
			}
			disk["access"].([]any)[0].(map[string]any)["permissions"] = candidate.permissions
			changed, err := json.Marshal(disk)
			if err != nil {
				t.Fatal(err)
			}
			state, err := decode(changed)
			if (err == nil) != candidate.accepted {
				t.Fatalf("accepted=%v error=%v", candidate.accepted, err)
			}
			if candidate.accepted && hasPermission(state[0], testWorkspace(t, "research")) {
				t.Fatal("empty relationship restored authority")
			}
		})
	}
	var disk map[string]any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	disk["schema_version"] = float64(1)
	old, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(old); err == nil {
		t.Fatal("obsolete Principal schema admitted")
	}
}
