package accessstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckedPermissionAuditCommitsWithAuthorityAndNeverDuplicatesReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	principal, _, err := store.Create("operator", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	preview, err := store.PreviewPermission(principal.ID, workspace, "", true, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	if _, err := store.ApplyPermissionChecked(t.Context(), principal.ID, workspace, preview.Revision, "audit-key", true, admit); err != nil {
		t.Fatal(err)
	}
	receipts := store.PermissionAudits()
	if len(receipts) != 1 || receipts[0].Audit.Principal != principal.ID.String() || receipts[0].Audit.Workspace != "research" || receipts[0].Audit.Action != "permission.grant" || receipts[0].Audit.Revision != preview.Revision || receipts[0].Audit.CommittedAt.IsZero() {
		t.Fatal("committed permission audit lost intent")
	}
	validReceipt := receipts[0]
	if err := validatePermissionReceipts([]PermissionReceipt{validReceipt}); err != nil {
		t.Fatal("published audit invalid", err)
	}
	wrong := *validReceipt.Audit
	wrong.Workspace = "coding"
	validReceipt.Audit = &wrong
	if err := validatePermissionReceipts([]PermissionReceipt{validReceipt}); err == nil {
		t.Fatal("audit resource diverged from retained intent digest")
	}
	receipts[0].Audit.Action = "tampered"
	if err := store.RevokePermission(principal.ID, workspace); err != nil {
		t.Fatal(err)
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reopened.ApplyPermissionChecked(t.Context(), principal.ID, workspace, preview.Revision, "audit-key", true, admit)
	if err != nil || !result.Replayed || len(reopened.PermissionAudits()) != 1 || reopened.PermissionAudits()[0].Audit.Action != "permission.grant" || hasPermission(reopened.List()[0], workspace) {
		t.Fatal("replay duplicated audit or restored authority", err)
	}
	denied := func() error { return errors.New("denied") }
	if _, err := reopened.ApplyPermissionChecked(t.Context(), principal.ID, workspace, preview.Revision, "audit-key", true, denied); err == nil {
		t.Fatal("receipt bypassed current admission")
	}
}

func TestPermissionAuditCodecRejectsMalformedFacts(t *testing.T) {
	digest := strings.Repeat("a", 64)
	withoutAudit := PermissionReceipt{Caller: "local", KeyHash: digest, IntentHash: digest}
	body, err := json.Marshal(diskState{SchemaVersion: SchemaVersion, Access: []diskAccess{}, PermissionReceipts: []PermissionReceipt{withoutAudit}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(body); err != nil {
		t.Fatal("current receipt without audit rejected", err)
	}
	principal, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := principal.Create("operator", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	preview, err := principal.PreviewPermission(identity.ID, workspace, "", true, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := principal.ApplyPermissionChecked(t.Context(), identity.ID, workspace, preview.Revision, "valid-audit", true, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	validReceipt := principal.PermissionAudits()[0]
	valid := *validReceipt.Audit
	if err := validatePermissionReceipts([]PermissionReceipt{validReceipt}); err != nil {
		t.Fatal("uncorrupted audit fixture must be accepted", err)
	}
	for _, corruption := range []string{"principal", "workspace", "action", "revision", "time", "expiry", "caller", "resource"} {
		t.Run(corruption, func(t *testing.T) {
			audit := valid
			receipt := validReceipt
			receipt.Audit = &audit
			switch corruption {
			case "principal":
				audit.Principal = "invalid"
			case "workspace":
				audit.Workspace = "Research"
			case "action":
				audit.Action = "permission.unknown"
			case "revision":
				audit.Revision = ""
			case "time":
				audit.CommittedAt = time.Time{}
			case "expiry":
				audit.Action = "permission.revoke"
				audit.ExpiresAt = time.Now()
			case "caller":
				receipt.Caller = ""
			case "resource":
				audit.Workspace = "coding"
			}
			body, err := json.Marshal(diskState{SchemaVersion: SchemaVersion, Access: []diskAccess{}, PermissionReceipts: []PermissionReceipt{receipt}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decode(body); err == nil {
				t.Fatal("invalid audit accepted")
			}
			path := filepath.Join(t.TempDir(), "principals.json")
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := openTestStore(path); err == nil {
				t.Fatal("invalid audit reopened")
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(unchanged, body) {
				t.Fatal("failed reopen rewrote authoritative bytes", err)
			}
		})
	}
}

func TestCheckedPermissionAuditFailedPublicationLeavesNoReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	principal, _, err := store.Create("operator", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	preview, err := store.PreviewPermission(principal.ID, workspace, "", true, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyPermissionChecked(t.Context(), principal.ID, workspace, preview.Revision, "audit-key", true, func() error { return nil }); err == nil {
		t.Fatal("obstructed publication succeeded")
	}
	if len(store.PermissionAudits()) != 0 || hasPermission(store.List()[0], workspace) {
		t.Fatal("failed publication exposed audit or authority")
	}
}

func TestPermissionAuditPreservesExpiryAndRevokeIntentAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	principal, _, err := store.Create("operator", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	expiresAt := time.Now().Add(time.Hour).In(time.FixedZone("review", 3600))
	admit := func() error { return nil }
	grant, err := store.PreviewPermission(principal.ID, workspace, "", true, expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyPermissionChecked(t.Context(), principal.ID, workspace, grant.Revision, "grant-key", true, admit, expiresAt); err != nil {
		t.Fatal(err)
	}
	revoke, err := store.PreviewPermission(principal.ID, workspace, "", false, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyPermissionChecked(t.Context(), principal.ID, workspace, revoke.Revision, "revoke-key", false, admit); err != nil {
		t.Fatal(err)
	}
	original := store.PermissionAudits()
	if len(original) != 2 || !original[0].Audit.ExpiresAt.Equal(expiresAt) || original[0].Audit.ExpiresAt.Location() != time.UTC || original[1].Audit.Action != "permission.revoke" || !original[1].Audit.ExpiresAt.IsZero() || original[1].Audit.Revision != revoke.Revision {
		t.Fatal("grant expiry or reviewed revoke facts lost")
	}
	if err := store.GrantPermission(principal.ID, workspace); err != nil {
		t.Fatal(err)
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reopened.ApplyPermissionChecked(t.Context(), principal.ID, workspace, revoke.Revision, "revoke-key", false, admit)
	if err != nil || !result.Replayed || !hasPermission(reopened.List()[0], workspace) {
		t.Fatal("revoke replay changed later authority", err)
	}
	before, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(reopened.PermissionAudits())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("reopen or replay changed committed audit facts", err)
	}
}
