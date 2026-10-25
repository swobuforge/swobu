package accounting

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func testQuote() Quote {
	zero := "0"
	return Quote{
		Price:    PriceSnapshot{Provider: "seller", ExternalID: "selector", ExternalVersion: "v1", Tariff: "regional", EffectiveAt: 100, MinInputTokens: 0, Currency: "USD", InputPerMillion: "1.000000000000000001", OutputPerMillion: "2", CacheReadPerMillion: &zero},
		TargetID: "target", TargetVersion: 1, AmountCents: 3, InputTokens: 10000, OutputTokens: 10000, QuantityCoverage: "partial",
	}
}

func TestReservationQuoteSurvivesSettlementReplayAndRestartWithoutAliasing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	quote := testQuote()
	expected := cloneQuote(&quote)
	reservation, err := store.ReserveQuote("coding", "attempt", quote)
	if err != nil {
		t.Fatal(err)
	}
	*quote.Price.CacheReadPerMillion = "99"
	reservation.Quote.Price.InputPerMillion = "99"
	*reservation.Quote.Price.CacheReadPerMillion = "99"
	projection := store.Reservations("coding")
	if len(projection) != 1 || !reflect.DeepEqual(projection[0].Quote, expected) {
		t.Fatal("caller mutated admitted quote")
	}
	*projection[0].Quote.Price.CacheReadPerMillion = "88"
	if err := store.SettleReservation("coding", reservation.WindowKey, "attempt", 9, false); err != nil {
		t.Fatal(err)
	}
	changed := testQuote()
	changed.Price.EffectiveAt = 200
	changed.AmountCents = 99
	replay, err := store.ReserveQuote("coding", "attempt", changed)
	if err != nil || replay.AmountCents != 9 || !reflect.DeepEqual(replay.Quote, expected) {
		t.Fatal("replay repriced historical attempt")
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	restored := reopened.Reservations("coding")
	if len(restored) != 1 || restored[0].AmountCents != 9 || restored[0].State != "settled" || !reflect.DeepEqual(restored[0].Quote, expected) {
		t.Fatal("restart lost quote provenance or original estimate")
	}
	if len(reopened.Reservations("research")) != 0 {
		t.Fatal("quote leaked to sibling Workspace")
	}
}

func TestInvalidQuoteCannotMutateAccounting(t *testing.T) {
	for _, field := range []string{"target", "version", "provider", "tariff", "currency", "rate", "quantity", "coverage", "band", "amount"} {
		t.Run(field, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounting.json")
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			quote := testQuote()
			switch field {
			case "target":
				quote.TargetID = ""
			case "version":
				quote.TargetVersion = 0
			case "provider":
				quote.Price.Provider = "seller "
			case "tariff":
				quote.Price.Tariff = ""
			case "currency":
				quote.Price.Currency = "EUR"
			case "rate":
				quote.Price.InputPerMillion = "NaN"
			case "quantity":
				quote.OutputTokens = -1
			case "coverage":
				quote.QuantityCoverage = "unknown"
			case "band":
				quote.Price.MinInputTokens = 10001
			case "amount":
				quote.AmountCents = -1
			}
			revision := store.Revision()
			if _, err := store.ReserveQuote("coding", "attempt", quote); err == nil {
				t.Fatal("invalid quote admitted")
			}
			if store.Revision() != revision || len(store.Reservations("coding")) != 0 {
				t.Fatal("invalid quote mutated accounting")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("invalid quote created accounting storage")
			}
		})
	}
}

func TestQuotedReservationPersistenceFailureRetainsPriorAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ReserveQuote("coding", "first", testQuote())
	if err != nil {
		t.Fatal(err)
	}
	before := store.Reservations("coding")
	revision := store.Revision()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveQuote("coding", "second", testQuote()); err == nil {
		t.Fatal("failed quoted write succeeded")
	}
	if !reflect.DeepEqual(before, store.Reservations("coding")) || store.Revision() != revision {
		t.Fatal("failed quoted write changed admission authority")
	}
	if err := store.SettleReservation("coding", first.WindowKey, "first", 99, false); err == nil {
		t.Fatal("failed settlement succeeded")
	}
	if !reflect.DeepEqual(before, store.Reservations("coding")) || len(store.SettlementCorrections("coding")) != 0 {
		t.Fatal("failed settlement changed quote or charge")
	}
}

func TestInvalidPersistedQuoteRejectsReopenWithoutRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	quote := testQuote()
	quote.QuantityCoverage = "complete-ish"
	raw, err := json.Marshal(persisted{Reservations: []Reservation{{Workspace: "coding", Operation: "attempt", WindowKey: "2026-10-13", AmountCents: 3, Coverage: CoverageKnown, State: "reserved", Quote: &quote}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("invalid persisted quote reopened")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(raw) {
		t.Fatal("invalid quote rejection rewrote history")
	}
}
