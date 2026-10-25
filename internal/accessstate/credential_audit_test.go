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

func TestCredentialRevocationAuditRetainsMetadataThroughPrincipalRevokeAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, issued, err := store.Create("operator", testWorkspace(t, "research"))
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	preview, err := store.PreviewCredentialRevocation(identity.ID, issued.Credential.ID, admit)
	if err != nil {
		t.Fatal(err)
	}
	result, replayed, err := store.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, "audit-key", admit)
	if err != nil || replayed {
		t.Fatal("credential removal failed", err)
	}
	audits := store.CredentialRevocationAudits()
	if len(audits) != 1 || audits[0].Principal != identity.ID.String() || audits[0].Credential != issued.Credential.ID.String() || audits[0].Workspace != "research" || !audits[0].CreatedAt.Equal(issued.Credential.CreatedAt) || audits[0].Revision != preview.Revision || audits[0].Caller != "local" || audits[0].CommittedAt.IsZero() {
		t.Fatal("reviewed credential facts lost")
	}
	for key, audit := range result.CredentialRevocationAudits {
		audit.Workspace = "tampered"
		result.CredentialRevocationAudits[key] = audit
	}
	if _, _, err := store.ApplyPrincipalRevocation(identity.ID, PrincipalRevision(store.List()[0]), "principal-key", admit); err != nil {
		t.Fatal(err)
	}
	store, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, replayed, err := store.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, "audit-key", admit); err != nil || !replayed {
		t.Fatal("credential replay failed", err)
	}
	if !reflect.DeepEqual(store.CredentialRevocationAudits(), audits) {
		t.Fatal("later Principal revoke or replay rewrote credential facts")
	}
	if _, _, err := store.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, "audit-key", func() error { return errors.New("denied") }); err == nil {
		t.Fatal("receipt bypassed current admission")
	}
	body, err := os.ReadFile(path)
	if err != nil || bytes.Contains(body, []byte(issued.Token)) || bytes.Contains(body, []byte("audit-key")) {
		t.Fatal("audit retained credential secret or retry key", err)
	}
}

func TestCredentialRevocationAuditRejectsContradictoryMetadataWithoutBackfill(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity, issued, err := store.Create("operator", testWorkspace(t, "research"))
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	preview, err := store.PreviewCredentialRevocation(identity.ID, issued.Credential.ID, admit)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, preview.Revision, "key", admit); err != nil {
		t.Fatal(err)
	}
	valid := store.List()[0]
	for _, corruption := range []string{"none", "principal", "credential", "workspace", "metadata", "caller", "revision", "created", "committed", "receipt", "historical"} {
		t.Run(corruption, func(t *testing.T) {
			candidate := cloneAccess(valid)
			for key, audit := range candidate.CredentialRevocationAudits {
				switch corruption {
				case "principal":
					audit.Principal = "local"
				case "credential":
					audit.Credential = "invalid"
				case "workspace":
					audit.Workspace = "Research"
				case "metadata":
					audit.Workspace = "coding"
				case "caller":
					audit.Caller = "foreign"
				case "revision":
					audit.Revision = "different"
				case "created":
					audit.CreatedAt = time.Time{}
				case "committed":
					audit.CommittedAt = time.Time{}
				case "receipt":
					delete(candidate.CredentialRevocationReceipts, key)
				}
				candidate.CredentialRevocationAudits[key] = audit
			}
			if corruption == "historical" {
				candidate.CredentialRevocationAudits = nil
			}
			body, err := encode([]Access{candidate})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "principals.json")
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			reopened, err := openTestStore(path)
			if corruption == "none" {
				if err != nil || len(reopened.CredentialRevocationAudits()) != 1 {
					t.Fatal("valid audit rejected", err)
				}
			} else if corruption == "historical" {
				if err != nil || len(reopened.CredentialRevocationAudits()) != 0 {
					t.Fatal("historical facts invented", err)
				}
			} else if err == nil {
				t.Fatal("corrupt audit reopened")
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(body, unchanged) {
				t.Fatal("reopen rewrote authority", err)
			}
		})
	}
}
