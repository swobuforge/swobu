package accessstate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPrincipalCreationRestartRevocationAndConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	identity, replayed, err := s.CreatePrincipalChecked(t.Context(), "Independent", "retained-creation-key", admit)
	if err != nil || replayed || len(identity.Credentials) != 0 || len(identity.Permissions) != 0 {
		t.Fatal("identity-only creation failed")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("retained-creation-key")) {
		t.Fatal("plaintext retry key persisted")
	}
	s, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	before := s.Revision()
	replay, replayed, err := s.CreatePrincipalChecked(t.Context(), "Independent", "retained-creation-key", admit)
	if err != nil || !replayed || replay.ID != identity.ID || s.Revision() != before || len(s.List()) != 1 {
		t.Fatal("restart replay changed identity or authority")
	}
	if _, _, err := s.CreatePrincipalChecked(t.Context(), "Changed", "retained-creation-key", admit); !errors.Is(err, ErrPrincipalIdempotencyConflict) {
		t.Fatal("changed creation intent did not conflict")
	}
	denied := errors.New("admission withdrawn")
	if _, _, err := s.CreatePrincipalChecked(t.Context(), "Independent", "retained-creation-key", func() error { return denied }); !errors.Is(err, denied) {
		t.Fatal("receipt bypassed admission")
	}
	if _, err := s.RevokeAccess(identity.ID); err != nil {
		t.Fatal(err)
	}
	s, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	replay, replayed, err = s.CreatePrincipalChecked(t.Context(), "Independent", "retained-creation-key", admit)
	if err != nil || !replayed || replay.ID != identity.ID || replay.RevokedAt == nil || len(replay.Credentials) != 0 || len(replay.Permissions) != 0 {
		t.Fatal("creation replay restored revoked authority")
	}
}

func TestPrincipalCreationFailureReservesNeitherIdentityNorKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	before := s.Revision()
	if _, _, err := s.CreatePrincipalChecked(t.Context(), "failed", "publication-key", admit); err == nil || len(s.List()) != 0 || s.Revision() != before {
		t.Fatal("failed publication retained identity or receipt")
	}
	if len(s.PrincipalCreationAudits()) != 0 {
		t.Fatal("failed publication exposed creation audit")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	identity, replayed, err := s.CreatePrincipalChecked(t.Context(), "different", "publication-key", admit)
	if err != nil || replayed || identity.Name != "different" {
		t.Fatal("failed publication reserved retry intent")
	}
}

func TestConcurrentPrincipalCreationPublishesOneIdentity(t *testing.T) {
	s, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.CreatePrincipalChecked(t.Context(), "concurrent", "same-key", func() error { return nil }); err != nil {
				t.Error("concurrent creation failed")
			}
		}()
	}
	wg.Wait()
	if len(s.List()) != 1 {
		t.Fatal("concurrent retry minted multiple identities")
	}
}
