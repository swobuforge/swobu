package accounting

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAccountingReopenRejectsCorruptClockAndReservationAuthority(t *testing.T) {
	for _, field := range []string{"floor", "floor-date", "coverage", "state", "workspace", "operation", "window", "duplicate"} {
		t.Run(field, func(t *testing.T) {
			reservation := Reservation{Workspace: "coding", Operation: "attempt", WindowKey: "2026-10-13", AmountCents: 3, Coverage: CoverageKnown, State: "reserved"}
			state := persisted{Limits: map[string]Limit{}, WindowFloor: "2026-10-13", Reservations: []Reservation{reservation}}
			switch field {
			case "floor":
				state.WindowFloor = "x"
			case "floor-date":
				state.WindowFloor = "2026-02-30"
			case "coverage":
				state.Reservations[0].Coverage = "free"
			case "state":
				state.Reservations[0].State = "ignored"
			case "workspace":
				state.Reservations[0].Workspace = ""
			case "operation":
				state.Reservations[0].Operation = ""
			case "window":
				state.Reservations[0].WindowKey = "2026-02-30"
			case "duplicate":
				state.Reservations = append(state.Reservations, reservation)
			}
			raw, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "accounting.json")
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path); err == nil {
				t.Fatal("corrupt accounting authority accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(raw) {
				t.Fatal("rejected authority was rewritten")
			}
		})
	}
}

func TestAccountingHistoricalUnknownCoverageReopensWithoutRewriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	raw := []byte(`{"limits":{},"reservations":[{"workspace":"coding","operation":"historical","window_key":"2026-10","amount_cents":3,"state":"uncertain"}]}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reservations := store.Reservations("coding")
	if len(reservations) != 1 || reservations[0].Coverage != CoverageUnknown || reservations[0].Quote != nil || reservations[0].AmountCents != 3 {
		t.Fatal("historical reservation gained fabricated coverage")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(raw) {
		t.Fatal("historical reopen rewrote authority")
	}
}

func TestAccountingRejectsIncompleteReservationBeforePersistence(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	before := store.Revision()
	for _, identity := range []struct{ workspace, operation string }{{"", "attempt"}, {"coding", ""}} {
		if _, err := store.Reserve(identity.workspace, identity.operation, 3, true); err == nil {
			t.Fatal("incomplete reservation published")
		}
		if store.Revision() != before {
			t.Fatal("invalid reservation moved authority or clock")
		}
	}
}
