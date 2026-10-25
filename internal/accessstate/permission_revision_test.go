package accessstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestPermissionConsentStaysStaleAfterRevokeAndIdenticalRegrant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	identity, _, err := store.Create("revision-round-trip", workspace)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.ReadPermission(identity.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokePermission(identity.ID, workspace); err != nil {
		t.Fatal(err)
	}
	if err := store.GrantPermission(identity.ID, workspace); err != nil {
		t.Fatal(err)
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ApplyPermissionChecked(t.Context(), identity.ID, workspace, before.Revision, "old-consent", false, func() error { return nil }); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("old consent after revoke/regrant=%v want revision conflict", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(persisted, after) || len(reopened.permissionReceipts) != 0 {
		t.Fatal("stale round-trip consent published authority or receipt")
	}
}

func TestRelationshipRevisionPreservesSiblingConsentAndRetainsAbsentHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	research := testWorkspace(t, "research")
	other := testWorkspace(t, "other")
	identity, _, err := store.Create("relationships", research)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PermissionRevision(identity.ID, research)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.GrantPermission(identity.ID, other); err != nil {
		t.Fatal(err)
	}
	unchanged, err := store.PermissionRevision(identity.ID, research)
	if err != nil || unchanged != revision {
		t.Fatalf("sibling edit invalidated consent: revision=%s err=%v", unchanged, err)
	}
	if _, err := store.IssueCredential(identity.ID, other); err != nil {
		t.Fatal(err)
	}
	if unchanged, err := store.PermissionRevision(identity.ID, research); err != nil || unchanged != revision {
		t.Fatal("sibling credential invalidated target consent")
	}
	if err := store.GrantPermission(identity.ID, research); err != nil {
		t.Fatal(err)
	}
	if unchanged, err := store.PermissionRevision(identity.ID, research); err != nil || unchanged != revision {
		t.Fatal("no-op grant invalidated target consent")
	}
	if err := store.RevokePermission(identity.ID, research); err != nil {
		t.Fatal(err)
	}
	absent, err := store.PermissionRevision(identity.ID, research)
	if err != nil || absent == revision {
		t.Fatal("revoke did not advance relationship revision")
	}
	if err := store.GrantPermission(identity.ID, research); err != nil {
		t.Fatal(err)
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reopened.PermissionRevision(identity.ID, research)
	if err != nil || current == revision || current == absent {
		t.Fatal("relationship revision history lost across regrant/reopen")
	}
	if _, err := reopened.IssueCredential(identity.ID, research); err != nil {
		t.Fatal(err)
	}
	withCredential, err := reopened.PermissionRevision(identity.ID, research)
	if err != nil || withCredential == current {
		t.Fatal("credential impact change did not invalidate relationship consent")
	}
	if _, err := reopened.RevokeAccess(identity.ID); err != nil {
		t.Fatal(err)
	}
	if revoked, err := reopened.PermissionRevision(identity.ID, research); err != nil || revoked == withCredential {
		t.Fatal("Principal revocation did not invalidate relationship consent")
	}
}

func TestCheckedRelationshipConsentAllowsIndependentEditsButOnlyOneCompetingEdit(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	research := testWorkspace(t, "research")
	other := testWorkspace(t, "other")
	identity, _, err := store.Create("concurrent-relations", research)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.GrantPermission(identity.ID, other); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, workspace := range []routing.WorkspaceSlug{research, other} {
		revision, err := store.PermissionRevision(identity.ID, workspace)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			<-start
			_, err := store.ApplyPermissionChecked(t.Context(), identity.ID, workspace, revision, workspace.String()+"-revoke", false, func() error { return nil })
			results <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("independent relationship edit rejected: %v", err)
		}
	}
	revision, err := store.PermissionRevision(identity.ID, research)
	if err != nil {
		t.Fatal(err)
	}
	start = make(chan struct{})
	for _, key := range []string{"competing-one", "competing-two"} {
		go func() {
			<-start
			_, err := store.ApplyPermissionChecked(t.Context(), identity.ID, research, revision, key, true, func() error { return nil })
			results <- err
		}()
	}
	close(start)
	accepted, stale := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrRevisionConflict):
			stale++
		default:
			t.Fatal(err)
		}
	}
	if accepted != 1 || stale != 1 || len(store.permissionReceipts) != 3 {
		t.Fatalf("accepted=%d stale=%d receipts=%d", accepted, stale, len(store.permissionReceipts))
	}
}

func TestRelationshipGenerationIsDetachedStrictAndFailClosedAtOverflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	identity, _, err := store.Create("generation-boundary", workspace)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := store.PermissionRevision(identity.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := store.List()
	snapshot[0].PermissionGenerations[workspace] = ^uint64(0)
	if current, _ := store.PermissionRevision(identity.ID, workspace); current != revision {
		t.Fatal("detached snapshot mutated retained generation")
	}
	if err := store.commit(snapshot); err != nil {
		t.Fatal(err)
	}
	if current, _ := store.PermissionRevision(identity.ID, workspace); current != revision {
		t.Fatal("publication accepted caller-authored generation")
	}
	store.state[0].PermissionGenerations[workspace] = ^uint64(0)
	if err := store.commit(store.List()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyPermission(identity.ID, workspace, store.Revision(), "relationship-overflow", false); err == nil {
		t.Fatal("relationship generation wrapped")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || len(store.permissionReceipts) != 0 {
		t.Fatal("overflow published authority or receipt")
	}
	var disk diskState
	if err := json.Unmarshal(before, &disk); err != nil {
		t.Fatal(err)
	}
	disk.Access[0].PermissionGenerations[workspace.String()] = 0
	invalid, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(invalid); err == nil {
		t.Fatal("codec accepted explicit zero generation")
	}
}

func TestFailedRelationshipPublicationPreservesRevisionAndRetry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	identity, _, err := store.Create("failed-publication", workspace)
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
	if err := store.RevokePermission(identity.ID, workspace); err == nil {
		t.Fatal("obstructed publication succeeded")
	}
	if current, _ := store.PermissionRevision(identity.ID, workspace); current != revision {
		t.Fatal("failed publication advanced relationship revision")
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
	if current, _ := reopened.PermissionRevision(identity.ID, workspace); current != revision {
		t.Fatal("failed publication advanced persisted revision")
	}
	if err := reopened.RevokePermission(identity.ID, workspace); err != nil {
		t.Fatal(err)
	}
	if current, _ := reopened.PermissionRevision(identity.ID, workspace); current == revision {
		t.Fatal("repaired publication did not advance revision")
	}
}

func TestAuthorityGenerationCannotBeSpoofedOrOverflowPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	identity, _, err := store.Create("generation", workspace)
	if err != nil {
		t.Fatal(err)
	}
	candidate := store.List()
	candidate[0].AuthorityGeneration = ^uint64(0)
	if err := store.commit(candidate); err != nil {
		t.Fatal(err)
	}
	if store.List()[0].AuthorityGeneration != identity.AuthorityGeneration {
		t.Fatal("snapshot spoofed authority generation")
	}
	// Exhaustion is a valid durable boundary, not a wrap to old consent.
	store.state[0].AuthorityGeneration = ^uint64(0)
	if err := store.commit(store.List()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyPermission(identity.ID, workspace, store.Revision(), "overflow", false); err == nil {
		t.Fatal("authority generation overflow published")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || len(store.permissionReceipts) != 0 || !hasPermission(store.List()[0], workspace) {
		t.Fatal("overflow changed authority or receipts")
	}
}
