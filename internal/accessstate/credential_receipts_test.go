package accessstate

import (
	"bytes"
	"errors"
	"github.com/swobuforge/swobu/internal/routing"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCredentialReceiptRestartRevocationAndChangedIntent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	principal, _, err := s.Create("fixture", workspace)
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	issued, replayed, err := s.IssueCredentialChecked(principal.ID, workspace, "stable-key", admit)
	if err != nil || replayed || issued.Token == "" {
		t.Fatal("initial issuance failed")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(issued.Token)) || bytes.Contains(raw, []byte("stable-key")) {
		t.Fatal("plaintext secret or key persisted")
	}
	s, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	replay, replayed, err := s.IssueCredentialChecked(principal.ID, workspace, "stable-key", admit)
	if err != nil || !replayed || replay.Token != "" || replay.Credential.ID != issued.Credential.ID || replay.Credential.Digest != [32]byte{} {
		t.Fatal("reopen replay exposed material or lost original identity")
	}
	if _, _, err := s.IssueCredentialChecked(principal.ID, testWorkspace(t, "coding"), "stable-key", admit); !errors.Is(err, ErrCredentialIdempotencyConflict) {
		t.Fatal("changed audience did not conflict")
	}
	denied := errors.New("admission withdrawn")
	if _, _, err := s.IssueCredentialChecked(principal.ID, workspace, "stable-key", func() error { return denied }); !errors.Is(err, denied) {
		t.Fatal("receipt bypassed admission")
	}
	if _, err := s.RevokeCredential(principal.ID, issued.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokeAccess(principal.ID); err != nil {
		t.Fatal(err)
	}
	s, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	replay, replayed, err = s.IssueCredentialChecked(principal.ID, workspace, "stable-key", admit)
	if err != nil || !replayed || replay.Token != "" || replay.Credential.ID != issued.Credential.ID {
		t.Fatal("revocation lost issuance receipt")
	}
	if _, err := s.Authenticate(issued.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("replay restored revoked authority")
	}
	if _, _, err := s.IssueCredentialChecked(principal.ID, workspace, "new-key", admit); !errors.Is(err, ErrRevoked) {
		t.Fatal("fresh issuance allowed revoked Principal")
	}
}

func TestCredentialReceiptConcurrentRetryPublishesOneSecret(t *testing.T) {
	s, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	principal, _, err := s.Create("fixture", workspace)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	secrets := make(chan string, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			issued, _, err := s.IssueCredentialChecked(principal.ID, workspace, "concurrent-key", func() error { return nil })
			if err != nil {
				t.Error(err)
				return
			}
			if issued.Token != "" {
				secrets <- issued.Token
			}
		}()
	}
	wg.Wait()
	close(secrets)
	if len(secrets) != 1 || len(s.List()[0].Credentials) != 2 {
		t.Fatal("concurrent retry minted multiple credentials")
	}
}

func TestCredentialReceiptFailedPublicationReservesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	s, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	principal, _, err := s.Create("fixture", workspace)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	previousPublish := s.publish
	s.publish = func([]byte) error { return errors.New("injected publication failure") }
	if _, _, err := s.IssueCredentialChecked(principal.ID, workspace, "repair-key", func() error { return nil }); err == nil {
		t.Fatal("fault did not fail")
	}
	s.publish = previousPublish
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || len(s.credentialReceipts) != 0 || len(s.List()[0].Credentials) != 1 {
		t.Fatal("failed publication changed authority or reserved key")
	}
	if len(s.CredentialIssuanceAudits()) != 0 {
		t.Fatal("failed publication exposed issuance audit")
	}
	issued, replayed, err := s.IssueCredentialChecked(principal.ID, workspace, "repair-key", func() error { return nil })
	if err != nil || replayed || issued.Token == "" {
		t.Fatal("repaired retry was not fresh issuance")
	}
}

func TestClientIssuanceRequiresReviewedCurrentModelsPermission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	workspace := testWorkspace(t, "research")
	principal, _, err := store.Create("fixture", workspace)
	if err != nil {
		t.Fatal(err)
	}
	original, err := store.PermissionRevision(principal.ID, workspace)
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	_, err = store.ApplyPermissionServicesChecked(t.Context(), principal.ID, workspace, original, "mcp-only-permission", true, admit, []routing.ServiceKind{routing.ServiceMCP})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if _, _, err := store.IssueClientCredentialChecked(principal.ID, workspace, "stale-reviewed-models", original, admit); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale Models consent admitted", err)
	}
	current, _ := store.PermissionRevision(principal.ID, workspace)
	if _, _, err := store.IssueClientCredentialChecked(principal.ID, workspace, "current-mcp-only-scope", current, admit); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("MCP-only scope admitted Models issuance", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("denied issuance mutated credential authority")
	}
}
