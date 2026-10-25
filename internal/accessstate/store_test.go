package accessstate

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func testWorkspace(t *testing.T, raw string) routing.WorkspaceSlug {
	t.Helper()
	slug, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		t.Fatal(err)
	}
	return slug
}

func TestCreatePersistsDigestAndAuthenticatesAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	access, issued, err := store.Create(" CI ", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(access.ID.String(), "acc_") || !strings.HasPrefix(issued.Credential.ID.String(), "acred_") || !strings.HasPrefix(issued.Token, "swak_") {
		t.Fatalf("unexpected IDs: %#v %#v", access, issued)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), issued.Token) {
		t.Fatal("plaintext token persisted")
	}
	digest := sha256.Sum256([]byte(issued.Token))
	if !strings.Contains(string(raw), encodeDigest(digest)) {
		t.Fatal("credential digest not persisted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := reopened.Authenticate(issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.AccessID != access.ID || resolution.Workspace.String() != "production" {
		t.Fatalf("resolution = %#v", resolution)
	}
}

func TestRotationRevocationAndWorkspaceReconciliation(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	access, first, err := store.Create("backend", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.GrantPermission(access.ID, testWorkspace(t, "staging")); err != nil {
		t.Fatal(err)
	}
	second, err := store.IssueCredential(access.ID, testWorkspace(t, "staging"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Authenticate(second.Token); got.AccessID != access.ID {
		t.Fatalf("rotated identity = %v", got.AccessID)
	}
	if _, err := store.RevokeCredential(access.ID, first.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(first.Token); err == nil {
		t.Fatal("revoked credential authenticated")
	}
	if _, err := store.Authenticate(second.Token); err != nil {
		t.Fatalf("sibling credential revoked: %v", err)
	}
	removed, err := store.RevokeWorkspace(testWorkspace(t, "staging"))
	if err != nil || !removed {
		t.Fatalf("RevokeWorkspace = %v, %v", removed, err)
	}
	if _, err := store.Authenticate(second.Token); err == nil {
		t.Fatal("workspace credential remained active")
	}
}

func TestDestroyedWorkspacePermissionCannotAuthorizeRecreatedSlug(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	slug := testWorkspace(t, "research")
	access, issued, err := store.Create("consumer", slug)
	if err != nil {
		t.Fatal(err)
	}
	// Permission-only state is still revoked explicitly after removing a bearer.
	if _, err := store.RevokeCredential(access.ID, issued.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.RevokeWorkspace(slug); err != nil || !changed {
		t.Fatalf("revoke permission: %v %v", changed, err)
	}
	store, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.IssueCredential(access.ID, slug); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("destroyed permission authorized recreated slug: %v", err)
	}
	if list := store.List(); len(list) != 1 || list[0].ID != access.ID || len(list[0].Permissions) != 0 {
		t.Fatalf("forensic identity or permission state=%#v", list)
	}
	if err := store.GrantPermission(access.ID, slug); err != nil {
		t.Fatal(err)
	}
	if _, err := store.IssueCredential(access.ID, slug); err != nil {
		t.Fatalf("fresh explicit permission did not authorize: %v", err)
	}
}

func TestPermissionLifecycleIsIndependentFromCredentialIssuance(t *testing.T) {
	store, _ := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	workspace := testWorkspace(t, "production")
	access, issued, err := store.Create("operator", workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokePermission(access.ID, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(issued.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked permission auth=%v", err)
	}
	if _, err := store.IssueCredential(access.ID, workspace); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("issued without permission=%v", err)
	}
	if err := store.GrantPermission(access.ID, workspace); err != nil {
		t.Fatal(err)
	}
	rotated, err := store.IssueCredential(access.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Token == issued.Token || len(store.List()[0].Credentials) != 2 {
		t.Fatalf("rotation=%#v", rotated)
	}
}

func TestPermissionMutationRejectsStaleRevision(t *testing.T) {
	store, _ := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	workspace := testWorkspace(t, "production")
	access, _, err := store.Create("operator", workspace)
	if err != nil {
		t.Fatal(err)
	}
	revision := store.Revision()
	if err := store.GrantPermission(access.ID, testWorkspace(t, "staging")); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckRevision(revision); err == nil {
		t.Fatal("stale revision accepted")
	}
}

func TestConcurrentReviewedPermissionGrantsAdmitOnlyOneRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := store.Create("operator", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	revision := store.Revision()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, slug := range []string{"staging", "research"} {
		workspace := testWorkspace(t, slug)
		go func() { <-start; results <- store.GrantPermissionAtRevision(identity.ID, workspace, revision) }()
	}
	close(start)
	success, conflict := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success/conflict=%d/%d", success, conflict)
	}
	if err := store.RevokePermissionAtRevision(identity.ID, testWorkspace(t, "production"), revision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revoke=%v", err)
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Revision() != store.Revision() {
		t.Fatal("revision did not survive reopen")
	}
	if err := reopened.GrantPermissionAtRevision(identity.ID, testWorkspace(t, "extra"), revision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("reopened stale grant=%v", err)
	}
}

func TestRevokeAccessPreservesIdentityAndPreventsIssuance(t *testing.T) {
	store, _ := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	access, issued, err := store.Create("backend", testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevokeAccess(access.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(issued.Token); err == nil {
		t.Fatal("revoked access authenticated")
	}
	if _, err := store.IssueCredential(access.ID, testWorkspace(t, "production")); err == nil {
		t.Fatal("issued for revoked access")
	}
	listed := store.List()
	if len(listed) != 1 || listed[0].RevokedAt == nil || len(listed[0].Credentials) != 0 {
		t.Fatalf("listed = %#v", listed)
	}
}

func TestRevokeWorkspacePreservesOtherWorkspaceCredentials(t *testing.T) {
	store, _ := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	access, missing, err := store.Create("backend", testWorkspace(t, "removed"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.GrantPermission(access.ID, testWorkspace(t, "production")); err != nil {
		t.Fatal(err)
	}
	retained, err := store.IssueCredential(access.ID, testWorkspace(t, "production"))
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := store.RevokeWorkspace(testWorkspace(t, "removed")); err != nil || !changed {
		t.Fatalf("RevokeWorkspace=%v,%v", changed, err)
	}
	if _, err := store.Authenticate(missing.Token); err == nil {
		t.Fatal("removed Workspace credential survived")
	}
	if _, err := store.Authenticate(retained.Token); err != nil {
		t.Fatalf("live credential revoked: %v", err)
	}
	if _, err := store.IssueCredential(access.ID, testWorkspace(t, "removed")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("removed permission survived: %v", err)
	}
	if _, err := store.IssueCredential(access.ID, testWorkspace(t, "production")); err != nil {
		t.Fatalf("live sibling permission removed: %v", err)
	}
}

func TestOpenRejectsCorruptUnsupportedOrFutureShapedState(t *testing.T) {
	for name, raw := range map[string]string{
		"corrupt":     `{`,
		"unsupported": `{"schema_version":4,"access":[]}`,
		"unknown":     `{"schema_version":3,"access":[],"future_field":true}`,
		"obsolete":    `{"schema_version":2,"access":[]}`,
		"trailing":    `{"schema_version":3,"access":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "principals.json")
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := openTestStore(path); err == nil {
				t.Fatal("Open unexpectedly succeeded")
			}
		})
	}
}

func encodeDigest(digest [32]byte) string { return base64.RawURLEncoding.EncodeToString(digest[:]) }
