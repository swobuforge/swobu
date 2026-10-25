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

func TestPrincipalCreationAuditRetainsOriginalIntentAcrossLabelAndRevoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	identity, replayed, err := store.CreatePrincipalChecked(t.Context(), "original", "creation-key", admit)
	if err != nil || replayed {
		t.Fatal("creation failed", err)
	}
	original := store.PrincipalCreationAudits()
	if len(original) != 1 || original[0].Principal != identity.ID.String() || original[0].Label != "original" || original[0].Caller != "local" || !original[0].CommittedAt.Equal(identity.CreatedAt) {
		t.Fatal("creation facts lost")
	}
	identity.CreationAudit.Label = "tampered"
	updated, err := store.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "later", PrincipalRevision(store.List()[0]), admit)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApplyPrincipalRevocation(identity.ID, PrincipalRevision(updated), "revoke-key", admit); err != nil {
		t.Fatal(err)
	}
	store, err = openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	result, replayed, err := store.CreatePrincipalChecked(t.Context(), "original", "creation-key", admit)
	if err != nil || !replayed || result.Name != "later" || result.RevokedAt == nil || !reflect.DeepEqual(original, store.PrincipalCreationAudits()) {
		t.Fatal("replay rewrote original facts or authority", err)
	}
	if _, _, err := store.CreatePrincipalChecked(t.Context(), "original", "creation-key", func() error { return errors.New("denied") }); err == nil {
		t.Fatal("receipt bypassed admission")
	}
}

func TestPrincipalCreationAuditRejectsCorruptionWithoutBackfill(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	valid, _, err := store.CreatePrincipalChecked(t.Context(), "original", "key", func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, corruption := range []string{"none", "principal", "caller", "label", "time", "receipt", "historical"} {
		t.Run(corruption, func(t *testing.T) {
			candidate := cloneAccess(valid)
			switch corruption {
			case "principal":
				candidate.CreationAudit.Principal = "local"
			case "caller":
				candidate.CreationAudit.Caller = "foreign"
			case "label":
				candidate.CreationAudit.Label = "different"
			case "time":
				candidate.CreationAudit.CommittedAt = time.Time{}
			case "receipt":
				candidate.CreationKeyHash, candidate.CreationIntentHash = "", ""
			case "historical":
				candidate.CreationAudit = nil
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
				if err != nil {
					t.Fatal("valid audit rejected", err)
				}
			} else if corruption == "historical" {
				if err != nil || len(reopened.PrincipalCreationAudits()) != 0 {
					t.Fatal("historical facts invented", err)
				}
			} else if err == nil {
				t.Fatal("corrupt audit accepted")
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(body, unchanged) {
				t.Fatal("failed reopen changed bytes", err)
			}
		})
	}
}
