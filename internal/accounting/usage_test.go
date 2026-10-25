package accounting

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func observedUsage(t *testing.T, input, output, read, write, reasoning int) canonical.TokenUsage {
	t.Helper()
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: &input, OutputTokens: &output, CacheReadTokens: &read, CacheWriteTokens: &write, ReasoningTokens: &reasoning})
	if err != nil {
		t.Fatal(err)
	}
	return usage
}

func TestObservedCostUsesSubsetDimensionsAndExactFrozenRates(t *testing.T) {
	quote := testQuote()
	quote.InputTokens = 1000000
	quote.Price.InputPerMillion = "1"
	quote.Price.OutputPerMillion = "2"
	readRate, writeRate, reasoningRate := "0.1", "3", "4"
	quote.Price.CacheReadPerMillion = &readRate
	quote.Price.CacheWritePerMillion = &writeRate
	quote.Price.ReasoningPerMillion = &reasoningRate
	amount, known := quote.ObservedUSDCents(observedUsage(t, 1000000, 1000000, 100000, 200000, 250000))
	if !known || amount != 381 {
		t.Fatalf("subset cost=%d known=%t want=381", amount, known)
	}
	for _, usage := range []canonical.TokenUsage{
		canonical.NewUnknownTokenUsage(),
		observedUsage(t, 1, 1, 2, 0, 0),
		observedUsage(t, 1, 1, 1, 1, 0),
		observedUsage(t, 1, 1, 0, 0, 2),
	} {
		if _, known := quote.ObservedUSDCents(usage); known {
			t.Fatal("incomplete/inconsistent usage priced")
		}
	}
	quote.Price.ReasoningPerMillion = nil
	quote.InputTokens = 10
	if _, known := quote.ObservedUSDCents(observedUsage(t, 11, 10, 0, 0, 0)); known {
		t.Fatal("changed input quantity reused an unproven band")
	}
	if _, known := quote.ObservedUSDCents(observedUsage(t, 10, 10, 0, 0, 1)); known {
		t.Fatal("unknown reasoning rate inherited ordinary output price")
	}
	if _, known := quote.ObservedUSDCents(observedUsage(t, 10, 10, 0, 0, 0)); !known {
		t.Fatal("known zero reasoning required nonexistent rate")
	}
}

func TestUsageSettlementRetainsQuoteAndUncertaintyAcrossRestart(t *testing.T) {
	for _, scenario := range []string{"known", "unknown", "failed"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounting.json")
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			quote := testQuote()
			quote.InputTokens = 1000000
			reservation, err := store.ReserveQuote("coding", "attempt", quote)
			if err != nil {
				t.Fatal(err)
			}
			usage := observedUsage(t, 1000000, 1000000, 0, 0, 0)
			if scenario == "unknown" {
				usage = canonical.NewUnknownTokenUsage()
			}
			if err := store.SettleUsage("coding", reservation.WindowKey, "attempt", usage, scenario != "failed"); err != nil {
				t.Fatal(err)
			}
			before := store.Reservations("coding")
			wantAmount, wantState := int64(3), "uncertain"
			if scenario == "known" {
				wantAmount, wantState = 301, "settled"
			}
			if len(before) != 1 || before[0].AmountCents != wantAmount || before[0].State != wantState || !reflect.DeepEqual(before[0].Quote, &quote) {
				t.Fatalf("settlement=%#v", before)
			}
			if err := store.SettleUsage("coding", reservation.WindowKey, "attempt", usage, scenario != "failed"); err != nil {
				t.Fatal(err)
			}
			if len(store.SettlementCorrections("coding")) != 0 {
				t.Fatal("identical terminal replay appended correction")
			}
			reopened, err := Open(path)
			if err != nil || !reflect.DeepEqual(before, reopened.Reservations("coding")) {
				t.Fatal("restart changed settled economics")
			}
		})
	}
}
