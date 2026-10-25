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

func TestPrincipalRevocationAuditAtomicReplayAndDetachedHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := store.Create("operator", testWorkspace(t, "research"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.GrantPermission(identity.ID, testWorkspace(t, "coding")); err != nil {
		t.Fatal(err)
	}
	identity = store.List()[0]
	review := PrincipalRevision(identity)
	admit := func() error { return nil }
	result, replayed, err := store.ApplyPrincipalRevocation(identity.ID, review, "original", admit)
	if err != nil || replayed || result.RevokedAt == nil {
		t.Fatal("checked revoke failed", err)
	}
	audits := store.PrincipalRevocationAudits()
	if len(audits) != 1 || audits[0].Principal != identity.ID.String() || audits[0].Caller != "local" || audits[0].Revision != review || !reflect.DeepEqual(audits[0].Workspaces, []string{"coding", "research"}) || audits[0].CommittedAt.IsZero() {
		t.Fatal("committed facts lost", audits)
	}
	original := audits[0]
	audits[0].Workspaces[0] = "tampered"
	for key, audit := range result.RevocationAudits {
		audit.Workspaces[0] = "tampered"
		result.RevocationAudits[key] = audit
	}
	store, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, replayed, err := store.ApplyPrincipalRevocation(identity.ID, review, "original", admit); err != nil || !replayed {
		t.Fatal("reopen replay failed", err)
	}
	original.Workspaces = []string{"coding", "research"}
	if !reflect.DeepEqual(store.PrincipalRevocationAudits(), []PrincipalRevocationAudit{original}) {
		t.Fatal("replay duplicated or detached mutation altered history")
	}
	if _, _, err := store.ApplyPrincipalRevocation(identity.ID, review, "original", func() error { return errors.New("denied") }); err == nil {
		t.Fatal("audit receipt bypassed admission")
	}
	before := PrincipalRevision(store.List()[0])
	if _, _, err := store.ApplyPrincipalRevocation(identity.ID, before, "noop", admit); err != nil {
		t.Fatal(err)
	}
	if PrincipalRevision(store.List()[0]) != before || len(store.PrincipalRevocationAudits()) != 2 {
		t.Fatal("audit-only write changed authority generation")
	}
	store, err = openTestStore(path)
	if err != nil {
		t.Fatal("empty-scope audit failed reopen", err)
	}
	retained := store.PrincipalRevocationAudits()
	if len(retained) != 2 || retained[1].Workspaces == nil || len(retained[1].Workspaces) != 0 || PrincipalRevision(store.List()[0]) != before {
		t.Fatal("empty-scope audit or authority revision changed on reopen")
	}
}

func TestPrincipalRevocationAuditRejectsCorruptionWithoutHistoricalBackfill(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := store.Create("operator", testWorkspace(t, "research"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApplyPrincipalRevocation(identity.ID, PrincipalRevision(identity), "key", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	valid := store.List()[0]
	for _, corruption := range []string{"principal", "caller", "revision", "time", "scope", "duplicate", "missing_scope", "receipt", "historical"} {
		t.Run(corruption, func(t *testing.T) {
			candidate := cloneAccess(valid)
			for key, audit := range candidate.RevocationAudits {
				switch corruption {
				case "principal":
					audit.Principal = "local"
				case "caller":
					audit.Caller = "foreign"
				case "revision":
					audit.Revision = "different"
				case "time":
					audit.CommittedAt = time.Time{}
				case "scope":
					audit.Workspaces = []string{"Research"}
				case "duplicate":
					audit.Workspaces = []string{"research", "research"}
				case "missing_scope":
					audit.Workspaces = nil
				case "receipt":
					delete(candidate.RevocationReceipts, key)
				}
				candidate.RevocationAudits[key] = audit
			}
			if corruption == "historical" {
				candidate.RevocationAudits = nil
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
			if corruption == "historical" {
				if err != nil || len(reopened.PrincipalRevocationAudits()) != 0 {
					t.Fatal("historical audit invented", err)
				}
			} else if err == nil {
				t.Fatal("corrupt audit accepted")
			}
			unchanged, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(body, unchanged) {
				t.Fatal("reopen rewrote authority", readErr)
			}
		})
	}
}
