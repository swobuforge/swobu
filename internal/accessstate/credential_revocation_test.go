package accessstate

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestCredentialRevocationConcurrentRetriesAndResourceScope(t *testing.T) {
	s, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity, issued, err := s.Create("retained", testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := s.IssueCredential(identity.ID, testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	preview, err := s.PreviewCredentialRevocation(identity.ID, issued.Credential.ID, admit)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		replayed bool
		err      error
	}
	results := make(chan result, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, replayed, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, "shared-key", admit)
			results <- result{replayed, err}
		}()
	}
	workers.Wait()
	close(results)
	effects := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if !result.replayed {
			effects++
		}
	}
	if effects != 1 {
		t.Fatal("concurrent exact retries repeated effect")
	}
	// The same raw key on a different canonical resource is a separate intent,
	// not replay of the first credential's receipt.
	siblingPreview, err := s.PreviewCredentialRevocation(identity.ID, sibling.Credential.ID, admit)
	if err != nil {
		t.Fatal(err)
	}
	current, replayed, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, sibling.Credential.ID, siblingPreview.Revision, "shared-key", admit)
	if err != nil || replayed || len(current.Credentials) != 0 || len(current.CredentialRevocationReceipts) != 2 {
		t.Fatal("receipt scope conflated credentials", err)
	}
}

func TestCredentialRevocationReceiptRestartAndCurrentStateReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, issued, err := s.Create("retained", testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := s.IssueCredential(identity.ID, testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	preview, err := s.PreviewCredentialRevocation(identity.ID, issued.Credential.ID, admit)
	if err != nil {
		t.Fatal(err)
	}
	key := "credential-revocation-private-retry-key"
	committed, replayed, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, key, admit)
	if err != nil || replayed || len(committed.Credentials) != 1 || committed.Credentials[0].ID != sibling.Credential.ID || !reflect.DeepEqual(committed.Permissions, identity.Permissions) || len(committed.CredentialRevocationReceipts) != 1 {
		t.Fatal("apply lost selected scope or atomic receipt", err)
	}
	if _, err := s.Authenticate(issued.Token); err == nil {
		t.Fatal("removed credential authenticated")
	}
	if _, err := s.Authenticate(sibling.Token); err != nil {
		t.Fatal("sibling lost authority")
	}
	for digest := range committed.CredentialRevocationReceipts {
		committed.CredentialRevocationReceipts[digest] = "detached"
	}
	updated, err := s.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "later", PrincipalRevision(s.List()[0]), admit)
	if err != nil {
		t.Fatal(err)
	}
	s, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), key) {
		t.Fatal("plaintext retry key persisted")
	}
	replayedState, replayed, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, key, admit)
	if err != nil || !replayed || !reflect.DeepEqual(replayedState, updated) {
		t.Fatal("replay did not return current state", err)
	}
	denied := errors.New("withdrawn")
	if _, _, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, key, func() error { return denied }); !errors.Is(err, denied) {
		t.Fatal("replay bypassed admission")
	}
	if _, _, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, "changed", key, admit); !errors.Is(err, ErrCredentialRevocationIdempotencyConflict) {
		t.Fatal("changed intent accepted")
	}
	if _, _, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, "fresh-key", admit); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("fresh request reused stale consent")
	}
	after, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("replay or rejection republished state")
	}
	revoked, err := s.RevokePrincipalChecked(identity.ID, PrincipalRevision(updated), admit)
	if err != nil {
		t.Fatal(err)
	}
	s, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	current, replayed, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, key, admit)
	if err != nil || !replayed || !reflect.DeepEqual(current, revoked) {
		t.Fatal("later Principal revocation lost recovery", err)
	}
}

func TestCredentialRevocationReceiptFailedPublicationDoesNotReserveKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, issued, err := s.Create("retained", testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	preview, err := s.PreviewCredentialRevocation(identity.ID, issued.Credential.ID, admit)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup := path + ".fixture-backup"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, "retry", admit); err == nil {
		t.Fatal("obstructed publication succeeded")
	}
	if !reflect.DeepEqual(s.List()[0], identity) {
		t.Fatal("failed publication changed memory or reserved key")
	}
	if len(s.CredentialRevocationAudits()) != 0 {
		t.Fatal("failed publication exposed credential audit")
	}
	disk, err := os.ReadFile(backup)
	if err != nil || !reflect.DeepEqual(before, disk) {
		t.Fatal("failed publication altered disk")
	}
	if _, err := s.Authenticate(issued.Token); err != nil {
		t.Fatal("failed publication withdrew authority")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	s, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, replayed, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, "retry", admit); err != nil || replayed {
		t.Fatal("failed write reserved retry key", err)
	}
}

func TestCredentialRevocationReceiptGrammarAndGenerationIsolation(t *testing.T) {
	s, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity, issued, err := s.Create("retained", testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	preview, err := s.PreviewCredentialRevocation(identity.ID, issued.Credential.ID, admit)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "two keys", strings.Repeat("k", 257)} {
		if _, _, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, key, admit); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	if _, _, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, "", "retry", admit); err == nil {
		t.Fatal("missing consent accepted")
	}
	if _, _, err := s.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, "retry", nil); err == nil {
		t.Fatal("missing admission accepted")
	}
	candidate := s.List()
	candidate[0].CredentialRevocationReceipts = map[string]string{strings.Repeat("a", 64): strings.Repeat("b", 64)}
	if err := s.commit(candidate); err != nil {
		t.Fatal(err)
	}
	if PrincipalRevision(s.List()[0]) != PrincipalRevision(identity) {
		t.Fatal("receipt-only publication changed authority generation")
	}
	for _, invalid := range []string{"short", strings.Repeat("A", 64), strings.Repeat("z", 64)} {
		for _, badKey := range []bool{true, false} {
			state := s.List()
			key, intent := strings.Repeat("a", 64), strings.Repeat("b", 64)
			if badKey {
				key = invalid
			} else {
				intent = invalid
			}
			state[0].CredentialRevocationReceipts = map[string]string{key: intent}
			raw, err := encode(state)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decode(raw); err == nil {
				t.Fatal("invalid receipt digest decoded")
			}
		}
	}
}

func TestCheckedCredentialRevocationPreservesSiblingAndPermissionAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, first, err := s.Create("retained", testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewCredentialRevocation(identity.ID, first.Credential.ID, admit)
	if err != nil || preview.Workspace != first.Credential.Workspace || preview.Revision == "" || preview.CreatedAt != first.Credential.CreatedAt {
		t.Fatal("preview lost exact scope")
	}
	after, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("preview published authority")
	}
	second, err := s.IssueCredential(identity.ID, testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "later", PrincipalRevision(s.List()[0]), admit)
	if err != nil {
		t.Fatal(err)
	}
	stable, err := s.PreviewCredentialRevocation(identity.ID, first.Credential.ID, admit)
	if err != nil || stable.Revision != preview.Revision {
		t.Fatal("sibling or label edit invalidated credential consent")
	}
	denied := errors.New("withdrawn")
	if _, err := s.RevokeCredentialChecked(identity.ID, first.Credential.ID, preview.Revision, func() error { return denied }); !errors.Is(err, denied) {
		t.Fatal("revocation bypassed admission")
	}
	if _, err := s.RevokeCredentialChecked(identity.ID, first.Credential.ID, "stale", admit); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale consent accepted")
	}
	committed, err := s.RevokeCredentialChecked(identity.ID, first.Credential.ID, preview.Revision, admit)
	if err != nil || committed.Name != updated.Name || len(committed.Credentials) != 1 || committed.Credentials[0].ID != second.Credential.ID || !reflect.DeepEqual(committed.Permissions, updated.Permissions) {
		t.Fatal("revocation changed sibling or permission")
	}
	s, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(first.Token); err == nil {
		t.Fatal("removed credential revived after restart")
	}
	if _, err := s.Authenticate(second.Token); err != nil {
		t.Fatal("sibling credential lost authority")
	}
	before, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokeCredentialChecked(identity.ID, first.Credential.ID, preview.Revision, admit); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("old consent accepted after restart")
	}
	after, err = os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("stale retry published authority")
	}
}

func TestCheckedCredentialRevocationFailedPublicationPreservesAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, issued, err := s.Create("retained", testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	preview, err := s.PreviewCredentialRevocation(identity.ID, issued.Credential.ID, admit)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup := path + ".fixture-backup"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokeCredentialChecked(identity.ID, issued.Credential.ID, preview.Revision, admit); err == nil {
		t.Fatal("obstructed publication succeeded")
	}
	if !reflect.DeepEqual(s.List()[0], identity) {
		t.Fatal("failed publication altered memory")
	}
	disk, err := os.ReadFile(backup)
	if err != nil || !reflect.DeepEqual(before, disk) {
		t.Fatal("failed publication altered persisted authority")
	}
	if _, err := s.Authenticate(issued.Token); err != nil {
		t.Fatal("failed publication withdrew credential")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokeCredentialChecked(identity.ID, issued.Credential.ID, preview.Revision, admit); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialConsentCannotCrossPrincipalRevocation(t *testing.T) {
	s, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity, issued, err := s.Create("retained", testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	preview, err := s.PreviewCredentialRevocation(identity.ID, issued.Credential.ID, admit)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := s.Create("other", testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewCredentialRevocation(other.ID, issued.Credential.ID, admit); !errors.Is(err, ErrNotFound) {
		t.Fatal("credential preview crossed Principal ownership")
	}
	if _, err := s.RevokePrincipalChecked(identity.ID, PrincipalRevision(identity), admit); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokeCredentialChecked(identity.ID, issued.Credential.ID, preview.Revision, admit); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("credential consent crossed Principal revocation")
	}
}
