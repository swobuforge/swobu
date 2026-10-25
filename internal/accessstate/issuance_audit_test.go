package accessstate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCredentialIssuanceAuditCommitsWithoutSecretRecoveryOrBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := store.Create("operator", testWorkspace(t, "research"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	admit := func() error { return nil }
	issued, replayed, err := store.IssueCredentialChecked(identity.ID, workspace, "audit-key", admit)
	if err != nil || replayed {
		t.Fatal("issuance failed", err)
	}
	original := store.CredentialIssuanceAudits()
	if len(original) != 1 || original[0].Principal != identity.ID.String() || original[0].Credential != issued.Credential.ID.String() || original[0].Workspace != "research" || original[0].Caller != "local" || original[0].CommittedAt.IsZero() || !original[0].CreatedAt.Equal(issued.Credential.CreatedAt) {
		t.Fatal("issuance facts lost")
	}
	if _, err := store.RevokeAccess(identity.ID); err != nil {
		t.Fatal(err)
	}
	store, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	replay, replayed, err := store.IssueCredentialChecked(identity.ID, workspace, "audit-key", admit)
	if err != nil || !replayed || replay.Token != "" || replay.Credential.Digest != ([32]byte{}) || !reflect.DeepEqual(store.CredentialIssuanceAudits(), original) {
		t.Fatal("replay changed audit or recovered secret", err)
	}
	if _, _, err := store.IssueCredentialChecked(identity.ID, workspace, "audit-key", func() error { return errors.New("denied") }); err == nil {
		t.Fatal("receipt bypassed admission")
	}
	corrupt := append([]CredentialReceipt{}, store.credentialReceipts...)
	corrupt[0].Workspace = "coding"
	body, err := encodeDocument(store.state, store.permissionReceipts, corrupt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openTestStore(path); err == nil {
		t.Fatal("contradictory audit accepted")
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(body, unchanged) {
		t.Fatal("failed reopen rewrote authority", err)
	}
	legacy := append([]CredentialReceipt{}, store.credentialReceipts...)
	legacy[0].CommittedAt = time.Time{}
	body, err = encodeDocument(store.state, store.permissionReceipts, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	store, err = openTestStore(path)
	if err != nil || len(store.CredentialIssuanceAudits()) != 0 {
		t.Fatal("historical audit backfilled", err)
	}
}
