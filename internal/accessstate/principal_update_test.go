package accessstate

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPrincipalLabelRevisionSurvivesRoundTripAndSiblingEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, issued, err := s.Create("before", testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	permissionETag := permissionRevision(identity, testWorkspace(t, "prod"))
	revision := PrincipalRevision(identity)
	if _, _, err := s.Create("sibling", testWorkspace(t, "prod")); err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	updated, err := s.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "after", revision, admit)
	if err != nil || updated.ID != identity.ID || PrincipalRevision(updated) == revision || !reflect.DeepEqual(updated.Credentials, identity.Credentials) || !reflect.DeepEqual(updated.Permissions, identity.Permissions) {
		t.Fatal("metadata update altered authority or revision scope")
	}
	if permissionETag != permissionRevision(updated, testWorkspace(t, "prod")) {
		t.Fatal("label update invalidated relationship consent")
	}
	if _, err := s.Authenticate(issued.Token); err != nil {
		t.Fatal("label change invalidated credential")
	}
	unchanged, err := s.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "after", PrincipalRevision(updated), admit)
	if err != nil || PrincipalRevision(unchanged) != PrincipalRevision(updated) {
		t.Fatal("no-op advanced revision")
	}
	back, err := s.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "before", PrincipalRevision(updated), admit)
	if err != nil || PrincipalRevision(back) == revision {
		t.Fatal("label round trip restored stale consent")
	}
	s, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "stale", revision, admit); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("reopened store accepted stale revision")
	}
	denied := errors.New("withdrawn")
	if _, err := s.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "denied", PrincipalRevision(back), func() error { return denied }); !errors.Is(err, denied) {
		t.Fatal("publication bypassed admission")
	}
}

func TestPrincipalLabelFailedPublicationPreservesRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := s.CreatePrincipalChecked(t.Context(), "before", "create-key", func() error { return nil })
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
	if _, err := s.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "after", PrincipalRevision(identity), func() error { return nil }); err == nil {
		t.Fatal("obstructed publication succeeded")
	}
	current := s.List()[0]
	if current.Name != identity.Name || PrincipalRevision(current) != PrincipalRevision(identity) || current.CreationKeyHash != identity.CreationKeyHash {
		t.Fatal("failed publication changed identity or receipt")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "after", PrincipalRevision(identity), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestPrincipalCheckedRevocationPreservesAttributionAndRejectsStaleConsent(t *testing.T) {
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
	revision := PrincipalRevision(identity)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewPrincipalRevocation(identity.ID, revision, admit)
	if err != nil || preview.ID != identity.ID || len(preview.Credentials) != 1 || len(preview.Permissions) != 1 {
		t.Fatal("preview lost destructive scope")
	}
	after, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("preview published state")
	}
	updated, err := s.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "edited", revision, admit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokePrincipalChecked(identity.ID, revision, admit); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale consent revoked Principal")
	}
	if _, err := s.Authenticate(issued.Token); err != nil {
		t.Fatal("stale rejection changed authority")
	}
	denied := errors.New("withdrawn")
	if _, err := s.RevokePrincipalChecked(identity.ID, PrincipalRevision(updated), func() error { return denied }); !errors.Is(err, denied) {
		t.Fatal("revocation bypassed admission")
	}
	revoked, err := s.RevokePrincipalChecked(identity.ID, PrincipalRevision(updated), admit)
	if err != nil || revoked.RevokedAt == nil || revoked.ID != identity.ID || revoked.Name != "edited" || len(revoked.Permissions) != 0 || len(revoked.Credentials) != 0 {
		t.Fatal("revocation failed to preserve identity and remove authority")
	}
	if _, err := s.Authenticate(issued.Token); err == nil {
		t.Fatal("revoked credential authenticated")
	}
	s, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokePrincipalChecked(identity.ID, PrincipalRevision(updated), admit); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("restart restored stale consent")
	}
	before, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := s.RevokePrincipalChecked(identity.ID, PrincipalRevision(revoked), admit)
	after, readErr := os.ReadFile(path)
	if err != nil || readErr != nil || PrincipalRevision(unchanged) != PrincipalRevision(revoked) || !reflect.DeepEqual(before, after) {
		t.Fatal("already revoked Principal was republished")
	}
}

func TestPrincipalCheckedRevocationFailedPublicationPreservesAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := s.CreatePrincipalChecked(t.Context(), "retained", "create-fixture", func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "prod")
	if err := s.GrantPermission(identity.ID, workspace); err != nil {
		t.Fatal(err)
	}
	issued, err := s.IssueCredential(identity.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	identity = s.List()[0]
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
	if _, err := s.RevokePrincipalChecked(identity.ID, PrincipalRevision(identity), func() error { return nil }); err == nil {
		t.Fatal("obstructed publication succeeded")
	}
	if !reflect.DeepEqual(s.List()[0], identity) {
		t.Fatal("failed publication changed memory or creation receipt")
	}
	disk, err := os.ReadFile(backup)
	if err != nil || !reflect.DeepEqual(before, disk) {
		t.Fatal("failed publication changed authoritative bytes")
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
	revoked, err := s.RevokePrincipalChecked(identity.ID, PrincipalRevision(identity), func() error { return nil })
	if err != nil || revoked.CreationKeyHash != identity.CreationKeyHash || revoked.CreationIntentHash != identity.CreationIntentHash {
		t.Fatal("repaired publication lost attribution receipt")
	}
	replayed, replay, err := s.CreatePrincipalChecked(t.Context(), "retained", "create-fixture", func() error { return nil })
	if err != nil || !replay || replayed.RevokedAt == nil {
		t.Fatal("creation replay resurrected authority")
	}
}

func TestPrincipalRevocationReceiptSurvivesRestartAndMetadataChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := s.Create("retained", testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	revision := PrincipalRevision(identity)
	revoked, replay, err := s.ApplyPrincipalRevocation(identity.ID, revision, "original-retry", admit)
	if err != nil || replay || revoked.RevokedAt == nil {
		t.Fatal("initial revocation failed")
	}
	updated, err := s.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "later", PrincipalRevision(revoked), admit)
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
	current, replay, err := s.ApplyPrincipalRevocation(identity.ID, revision, "original-retry", admit)
	if err != nil || !replay || current.Name != "later" || PrincipalRevision(current) != PrincipalRevision(updated) {
		t.Fatal("replay did not report current retained identity")
	}
	if _, _, err := s.ApplyPrincipalRevocation(identity.ID, PrincipalRevision(updated), "original-retry", admit); !errors.Is(err, ErrPrincipalIdempotencyConflict) {
		t.Fatal("changed original intent accepted")
	}
	denied := errors.New("withdrawn")
	if _, _, err := s.ApplyPrincipalRevocation(identity.ID, revision, "original-retry", func() error { return denied }); !errors.Is(err, denied) {
		t.Fatal("receipt bypassed admission")
	}
	after, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("replay or rejection published state")
	}
	noop, replay, err := s.ApplyPrincipalRevocation(identity.ID, PrincipalRevision(updated), "second-retry", admit)
	if err != nil || replay || PrincipalRevision(noop) != PrincipalRevision(updated) {
		t.Fatal("receipt-only publication advanced authority")
	}
	noop.RevocationReceipts["spoof"] = "spoof"
	if _, exists := s.List()[0].RevocationReceipts["spoof"]; exists {
		t.Fatal("detached receipt map aliases authority")
	}
	bytes, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(bytes), "original-retry") || strings.Contains(string(bytes), "second-retry") {
		t.Fatal("retry key persisted in plaintext")
	}
}

func TestPrincipalRevocationReceiptFailedPublicationReservesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, issued, err := s.Create("retained", testWorkspace(t, "prod"))
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
	admit := func() error { return nil }
	if _, _, err := s.ApplyPrincipalRevocation(identity.ID, PrincipalRevision(identity), "retry", admit); err == nil {
		t.Fatal("failed publication succeeded")
	}
	if !reflect.DeepEqual(s.List()[0], identity) {
		t.Fatal("failed publication retained effect or receipt")
	}
	if len(s.PrincipalRevocationAudits()) != 0 {
		t.Fatal("failed publication exposed revocation audit")
	}
	disk, err := os.ReadFile(backup)
	if err != nil || !reflect.DeepEqual(before, disk) {
		t.Fatal("failed publication changed persisted authority")
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
	if _, replay, err := s.ApplyPrincipalRevocation(identity.ID, PrincipalRevision(identity), "retry", admit); err != nil || replay {
		t.Fatal("failed write reserved retry key")
	}
	s, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, replay, err := s.ApplyPrincipalRevocation(identity.ID, PrincipalRevision(identity), "retry", admit); err != nil || !replay {
		t.Fatal("reopened receipt missing")
	}
}

func TestPrincipalRevocationReceiptClosedPersistenceGrammar(t *testing.T) {
	s, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := s.Create("retained", testWorkspace(t, "prod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []map[string]string{{"bad": strings.Repeat("a", 64)}, {strings.Repeat("a", 64): "bad"}, {strings.Repeat("A", 64): strings.Repeat("a", 64)}} {
		candidate := cloneAccess(identity)
		now := s.now().UTC()
		candidate.RevokedAt = &now
		candidate.Credentials, candidate.Permissions = nil, nil
		candidate.RevocationReceipts = malformed
		encoded, err := encode([]Access{candidate})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decode(encoded); err == nil {
			t.Fatal("malformed receipt admitted")
		}
	}
	identity.RevocationReceipts = map[string]string{strings.Repeat("a", 64): strings.Repeat("b", 64)}
	encoded, err := encode([]Access{identity})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(encoded); err == nil {
		t.Fatal("active identity carried committed revocation receipt")
	}
}
