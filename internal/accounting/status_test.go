package accounting

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAccountingStatusPreservesExactTotalsAndReadsWithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) }
	if err := store.SetLimit("coding", Limit{Currency: "USD", AmountCents: math.MaxInt64, Window: "day", Mode: ModeObserveOnly, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	first, err := store.Reserve("coding", "first", math.MaxInt64, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reserve("coding", "second", math.MaxInt64, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SettleReservation("coding", first.WindowKey, "first", math.MaxInt64, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reserve("coding", "unknown", 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reserve("research", "other", 100, true); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	revision := store.Revision()
	status := store.Status("coding")
	if status.ChargedCents != "18446744073709551614" || status.ReservedCents != "9223372036854775807" || status.UncertainCents != "9223372036854775807" || status.SettledCents != "0" || status.RemainingCents == nil || *status.RemainingCents != "0" || status.UnknownPriceOperations != 1 || status.UnquotedOperations != 3 {
		t.Fatalf("exact status=%#v", status)
	}
	status.Limit.AmountCents = 1
	if store.Status("coding").Limit.AmountCents != math.MaxInt64 {
		t.Fatal("projection mutated limit")
	}
	store.now = func() time.Time { return time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC) }
	if next := store.Status("coding"); next.ChargedCents != "0" || next.WindowKey != "2026-10-14" {
		t.Fatal("new UTC period reused prior spend")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) || revision != store.Revision() {
		t.Fatal("status read mutated accounting or clock floor")
	}
}

func TestAccountingStatusMonthlyWindowAndQuoteCoverage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, 10, 31, 23, 0, 0, 0, time.UTC) }
	if err := store.SetLimit("coding", Limit{Currency: "USD", AmountCents: 100, Window: "month", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	quote := testQuote()
	reservation, err := store.ReserveQuote("coding", "attempt", quote)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SettleReservation("coding", reservation.WindowKey, "attempt", 10, false); err != nil {
		t.Fatal(err)
	}
	status := store.Status("coding")
	if status.WindowKey != "2026-10" || status.WindowStart.Day() != 1 || status.WindowEnd.Month() != time.November || status.SettledCents != "10" || status.ChargedCents != "10" || status.PartialQuantityOperations != 1 || status.UnquotedOperations != 0 || *status.RemainingCents != "90" {
		t.Fatalf("month/coverage=%#v", status)
	}
	store.now = func() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) }
	if status := store.Status("coding"); status.WindowKey != "2026-10" {
		t.Fatal("clock rollback changed accounting window")
	}
	if status := store.Status("unconfigured"); status.Limit != nil || status.RemainingCents != nil || status.ChargedCents != "0" {
		t.Fatal("missing limit invented budget")
	}
}

func TestAccountingStatusUsesUTCAndPersistedFloorAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time {
		return time.Date(2026, 11, 1, 1, 30, 0, 0, time.FixedZone("east", 2*60*60))
	}
	reservation, err := store.Reserve("coding", "attempt", 7, false)
	if err != nil {
		t.Fatal(err)
	}
	status := store.Status("coding")
	if status.WindowKey != "2026-10-31" || status.WindowStart.Location() != time.UTC || status.WindowEnd != time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) || status.ChargedCents != "7" || status.UnknownPriceOperations != 1 || status.Limit != nil || status.RemainingCents != nil {
		t.Fatalf("UTC/unconfigured status=%#v", status)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reopened.now = func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }
	if restored := reopened.Status("coding"); restored.WindowKey != status.WindowKey || restored.ChargedCents != status.ChargedCents || restored.UnknownPriceOperations != 1 {
		t.Fatalf("restart rollback status=%#v", restored)
	}
	reopened.now = func() time.Time { return time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) }
	if err := reopened.SettleReservation("coding", reservation.WindowKey, reservation.Operation, 9, true); err != nil {
		t.Fatal(err)
	}
	if current := reopened.Status("coding"); current.ChargedCents != "0" || current.UnknownPriceOperations != 0 || current.WindowKey != "2026-11-01" {
		t.Fatalf("prior-window settlement leaked into current status=%#v", current)
	}
}
