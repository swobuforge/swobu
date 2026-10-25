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

func TestPrincipalLabelAuditPreservesRoundTripAndRejectsNoopStaleFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	admit := func() error { return nil }
	identity, _, err := store.CreatePrincipalChecked(t.Context(), "original", "creation", admit)
	if err != nil {
		t.Fatal(err)
	}
	review := PrincipalRevision(identity)
	updated, err := store.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "later", review, admit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "stale", review, admit); err == nil {
		t.Fatal("stale edit accepted")
	}
	if _, err := store.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "later", PrincipalRevision(updated), admit); err != nil {
		t.Fatal(err)
	}
	if len(store.PrincipalLabelAudits()) != 1 {
		t.Fatal("stale/noop added facts")
	}
	if _, err := store.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "original", PrincipalRevision(updated), admit); err != nil {
		t.Fatal(err)
	}
	original := store.PrincipalLabelAudits()
	if len(original) != 2 || original[0].Before != "original" || original[0].After != "later" || original[0].Revision != review || original[1].Before != "later" || original[1].After != "original" {
		t.Fatal("round-trip history lost")
	}
	store, err = openTestStore(path)
	if err != nil || !reflect.DeepEqual(store.PrincipalLabelAudits(), original) {
		t.Fatal("reopen changed history", err)
	}
	before := store.List()[0]
	detached := store.List()[0]
	detached.LabelAudits[0].After = "tampered"
	store.publish = func([]byte) error { return errors.New("injected publication failure") }
	if _, err := store.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "failed", PrincipalRevision(before), admit); err == nil {
		t.Fatal("failed write acknowledged")
	}
	if !reflect.DeepEqual(before, store.List()[0]) || !reflect.DeepEqual(store.PrincipalLabelAudits(), original) {
		t.Fatal("failed write/detached snapshot changed facts")
	}
}

func TestPrincipalLabelAuditRejectsCorruptionWithoutHistoricalBackfill(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := store.CreatePrincipalChecked(t.Context(), "original", "creation", func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	valid, err := store.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "later", PrincipalRevision(identity), func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, corruption := range []string{"none", "principal", "caller", "before", "after", "revision", "generation", "time", "duplicate", "historical"} {
		t.Run(corruption, func(t *testing.T) {
			candidate := cloneAccess(valid)
			switch corruption {
			case "principal":
				candidate.LabelAudits[0].Principal = "local"
			case "caller":
				candidate.LabelAudits[0].Caller = "foreign"
			case "before":
				candidate.LabelAudits[0].Before = ""
			case "after":
				candidate.LabelAudits[0].After = candidate.LabelAudits[0].Before
			case "revision":
				candidate.LabelAudits[0].Revision = "wrong"
			case "generation":
				candidate.LabelAudits[0].Generation = candidate.AuthorityGeneration
			case "time":
				candidate.LabelAudits[0].CommittedAt = time.Time{}
			case "duplicate":
				candidate.LabelAudits = append(candidate.LabelAudits, candidate.LabelAudits[0])
			case "historical":
				candidate.LabelAudits = nil
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
				if err != nil || len(reopened.PrincipalLabelAudits()) != 0 {
					t.Fatal("historical facts invented", err)
				}
			} else if err == nil {
				t.Fatal("corrupt audit admitted")
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(body, unchanged) {
				t.Fatal("reopen changed bytes", err)
			}
		})
	}
}
