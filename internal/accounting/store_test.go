package accounting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFailedLimitPersistencePreservesPublishedSnapshot(t *testing.T) {
	for _, checked := range []bool{false, true} {
		t.Run(fmt.Sprintf("revision-checked=%t", checked), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounting.json")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			limit := Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
			if err := s.SetLimit("research", limit); err != nil {
				t.Fatal(err)
			}
			revision := s.Revision()
			if err := os.Rename(path, path+".saved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			changed := limit
			changed.AmountCents = 900
			if checked {
				err = s.SetLimitAtRevision("research", changed, revision)
			} else {
				err = s.SetLimit("research", changed)
			}
			if err == nil {
				t.Fatal("write obstruction did not fail")
			}
			got, _ := s.Limit("research")
			if got != limit || s.Revision() != revision {
				t.Fatal("failed write published new authority")
			}
		})
	}
}

func TestAccountingRejectsUnknownPricePolicyWithoutChangingDurableLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	valid := Limit{Currency: "USD", AmountCents: 1000, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
	if err := store.SetLimit("coding", valid); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	revision := store.Revision()
	for _, policy := range []string{"", "allow", "observe_only", "Reject", "reject "} {
		t.Run(policy, func(t *testing.T) {
			invalid := valid
			invalid.UnknownPrice = policy
			if err := store.SetLimit("coding", invalid); err == nil {
				t.Fatal("invalid unknown-price policy accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) || store.Revision() != revision {
				t.Fatal("invalid policy changed persisted authority")
			}
			if _, err := store.Reserve("coding", "unpriced", 0, false); !errors.Is(err, ErrUnknownPrice) {
				t.Fatal("invalid edit bypassed unknown-price enforcement")
			}
		})
	}
}

func TestAccountingReopenRejectsInvalidUnknownPricePolicy(t *testing.T) {
	for _, policy := range []string{"", "allow", "observe_only", "Reject", "reject "} {
		t.Run(policy, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounting.json")
			state := persisted{Limits: map[string]Limit{"coding": {Currency: "USD", AmountCents: 1000, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: policy}}}
			raw, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path); err == nil {
				t.Fatal("invalid persisted policy reopened")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(raw) != string(after) {
				t.Fatal("rejection rewrote invalid accounting state")
			}
		})
	}
}

func TestZeroBudgetThresholdCannotPublishOrReopen(t *testing.T) {
	for _, mode := range []Mode{ModeStopNewBillableWork, ModeObserveOnly} {
		t.Run(string(mode), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounting.json")
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			limit := Limit{Currency: "USD", AmountCents: 1, Window: "day", Mode: mode, UnknownPrice: "reject"}
			if err := store.SetLimit("coding", limit); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			revision := store.Revision()
			limit.AmountCents = 0
			if err := store.SetLimit("coding", limit); err == nil {
				t.Fatal("zero threshold published")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) || store.Revision() != revision {
				t.Fatal("zero threshold changed durable limit")
			}
			raw, err := json.Marshal(persisted{Limits: map[string]Limit{"coding": limit}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path); err == nil {
				t.Fatal("zero threshold reopened")
			}
			after, err = os.ReadFile(path)
			if err != nil || string(raw) != string(after) {
				t.Fatal("invalid threshold rejection rewrote state")
			}
		})
	}
}

func TestPositiveBudgetStillAllowsKnownZeroCostOperation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetLimit("coding", Limit{Currency: "USD", AmountCents: 1, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	reservation, err := store.Reserve("coding", "free", 0, true)
	if err != nil || reservation.Coverage != CoverageKnown || reservation.AmountCents != 0 {
		t.Fatal("positive threshold rejected known free operation")
	}
	if err := store.SettleReservation("coding", reservation.WindowKey, reservation.Operation, 0, false); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Reserve("coding", "paid", 1, true); err != nil {
		t.Fatal("free operation consumed paid budget after reopen")
	}
}

func TestNegativeSettlementCannotCreateBudgetCredit(t *testing.T) {
	for _, explicitWindow := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit-window=%t", explicitWindow), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounting.json")
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			limit := Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
			if err := store.SetLimit("coding", limit); err != nil {
				t.Fatal(err)
			}
			reservation, err := store.Reserve("coding", "attempt", 100, true)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if explicitWindow {
				err = store.SettleReservation("coding", reservation.WindowKey, "attempt", -1, false)
			} else {
				err = store.Settle("coding", "attempt", -1, false)
			}
			if err == nil {
				t.Fatal("negative settlement created budget credit")
			}
			store.now = func() time.Time { return time.Now().UTC().Add(48 * time.Hour) }
			if err := store.Settle("coding", "attempt", -1, true); err == nil {
				t.Fatal("negative settlement advanced accounting clock")
			}
			store.now = time.Now
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) || len(store.SettlementCorrections("coding")) != 0 {
				t.Fatal("invalid settlement changed durable accounting")
			}
			if _, err := store.Reserve("coding", "next", 1, true); !errors.Is(err, ErrBudgetExceeded) {
				t.Fatal("negative settlement reopened exhausted budget")
			}
		})
	}
}

func TestWorkspaceSpendComparisonDoesNotOverflow(t *testing.T) {
	for _, scenario := range []string{"proposed-addition", "observed-overshoot"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounting.json")
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			limit := Limit{Currency: "USD", AmountCents: math.MaxInt64, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
			if scenario == "observed-overshoot" {
				limit.Mode = ModeObserveOnly
			}
			if err := store.SetLimit("coding", limit); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Reserve("coding", "first", math.MaxInt64, true); err != nil {
				t.Fatal(err)
			}
			if replay, err := store.Reserve("coding", "first", math.MaxInt64, true); err != nil || replay.AmountCents != math.MaxInt64 {
				t.Fatal("overflow-safe comparison rejected existing reservation replay")
			}
			if scenario == "observed-overshoot" {
				if _, err := store.Reserve("coding", "second", math.MaxInt64, true); err != nil {
					t.Fatal(err)
				}
				limit.Mode = ModeStopNewBillableWork
				if err := store.SetLimit("coding", limit); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Reserve("coding", "denied", 1, true); !errors.Is(err, ErrBudgetExceeded) {
				t.Fatal("overflow admitted work beyond Workspace threshold")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) {
				t.Fatal("overflow denial changed durable spend")
			}
			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reopened.Reserve("coding", "after-restart", 1, true); !errors.Is(err, ErrBudgetExceeded) {
				t.Fatal("restart lost overflow-safe admission")
			}
			if _, err := reopened.Reserve("research", "independent", 1, true); err != nil {
				t.Fatal("overspent Workspace blocked sibling Workspace")
			}
		})
	}
}

func TestAccountingReopenRejectsNegativePersistedCharge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	state := persisted{Reservations: []Reservation{{Workspace: "coding", WindowKey: "2026-10-13", Operation: "attempt", AmountCents: -1, Coverage: CoverageKnown, State: "settled"}}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("negative persisted charge reopened as budget credit")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(raw) != string(after) {
		t.Fatal("negative charge rejection rewrote accounting history")
	}
}

func TestStoreReservesAtomicallyAndRejectsUnknownPrice(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLimit("research", Limit{Currency: "USD", AmountCents: 1000, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve("research", "op-unknown", 0, false); !errors.Is(err, ErrUnknownPrice) {
		t.Fatalf("unknown price err=%v", err)
	}
	if _, err := s.Reserve("research", "op-1", 700, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve("research", "op-2", 400, true); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("budget err=%v", err)
	}
	if _, err := s.Reserve("research", "op-1", 700, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Settle("research", "op-1", 600, false); err != nil {
		t.Fatal(err)
	}
}

func TestObserveOnlyUnknownPricePersistsUnknownCoverage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLimit("research", Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: ModeObserveOnly, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	reservation, err := s.Reserve("research", "unknown-price", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.AmountCents != 0 || reservation.Coverage != CoverageUnknown {
		t.Fatalf("unknown-price reservation = %#v", reservation)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reservations := reopened.Reservations("research")
	if len(reservations) != 1 || reservations[0].Coverage != CoverageUnknown {
		t.Fatalf("persisted unknown coverage = %#v", reservations)
	}
}

func TestLegacyReservationWithoutCoverageFailsClosedAsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	legacy := `{"limits":{},"reservations":[{"workspace":"research","window_key":"2026-10-12","operation":"legacy","amount_cents":5,"state":"reserved"}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reservations := s.Reservations("research")
	if len(reservations) != 1 || reservations[0].Coverage != CoverageUnknown || !reservations[0].Coverage.Valid() {
		t.Fatalf("legacy reservation coverage = %#v", reservations)
	}
}

func TestSettlementReplayIsIdempotentAndChangedSettlementIsAppendOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve("research", "exchange-1", 100, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Settle("research", "exchange-1", 80, false); err != nil {
		t.Fatal(err)
	}
	revision := s.Revision()
	if err := s.Settle("research", "exchange-1", 80, false); err != nil {
		t.Fatal(err)
	}
	if got := s.Revision(); got != revision {
		t.Fatalf("identical settlement replay changed revision: before=%s after=%s", revision, got)
	}
	if corrections := s.SettlementCorrections("research"); len(corrections) != 0 {
		t.Fatalf("identical settlement replay appended corrections: %#v", corrections)
	}
	if err := s.Settle("research", "exchange-1", 70, true); err != nil {
		t.Fatal(err)
	}
	corrections := s.SettlementCorrections("research")
	if len(corrections) != 1 || corrections[0].WindowKey == "" || corrections[0].PreviousCents != 80 || corrections[0].CorrectedCents != 70 || corrections[0].PreviousState != "settled" || corrections[0].CorrectedState != "uncertain" {
		t.Fatalf("corrections=%#v", corrections)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.SettlementCorrections("research")) != 1 {
		t.Fatal("settlement correction was not durable")
	}
}

func TestSettlementUsesCurrentWindowWhenOperationIsReused(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	firstDay := time.Date(2026, 10, 11, 23, 59, 0, 0, time.UTC)
	secondDay := firstDay.Add(2 * time.Minute)
	s.now = func() time.Time { return firstDay }
	if _, err := s.Reserve("research", "reused-operation", 10, true); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return secondDay }
	if _, err := s.Reserve("research", "reused-operation", 20, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Settle("research", "reused-operation", 18, false); err != nil {
		t.Fatal(err)
	}
	for _, reservation := range s.state.Reservations {
		if reservation.WindowKey == "2026-10-11" && (reservation.State != "reserved" || reservation.AmountCents != 10) {
			t.Fatalf("prior window changed: %#v", reservation)
		}
		if reservation.WindowKey == "2026-10-12" && (reservation.State != "settled" || reservation.AmountCents != 18) {
			t.Fatalf("current window was not settled: %#v", reservation)
		}
	}
}

func TestExplicitReservationWindowSettlesAcrossMidnight(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	firstDay := time.Date(2026, 10, 11, 23, 59, 0, 0, time.UTC)
	s.now = func() time.Time { return firstDay }
	reservation, err := s.Reserve("research", "cross-midnight", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return firstDay.Add(2 * time.Minute) }
	if err := s.SettleReservation("research", reservation.WindowKey, reservation.Operation, 9, false); err != nil {
		t.Fatal(err)
	}
	reservations := s.Reservations("research")
	if len(reservations) != 1 || reservations[0].WindowKey != "2026-10-11" || reservations[0].State != "settled" || reservations[0].AmountCents != 9 {
		t.Fatalf("cross-midnight settlement = %#v", reservations)
	}
}

func TestSettlementCorrectionsRetainReservationWindow(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	firstDay := time.Date(2026, 10, 11, 23, 59, 0, 0, time.UTC)
	s.now = func() time.Time { return firstDay }
	reservation, err := s.Reserve("research", "correction-window", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SettleReservation("research", reservation.WindowKey, reservation.Operation, 9, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleReservation("research", reservation.WindowKey, reservation.Operation, 8, true); err != nil {
		t.Fatal(err)
	}
	corrections := s.SettlementCorrections("research")
	if len(corrections) != 1 || corrections[0].WindowKey != reservation.WindowKey {
		t.Fatalf("correction identity = %#v", corrections)
	}
}

func TestConcurrentReservationsAdmitOnlyOneLastDollar(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	limit := Limit{Currency: "USD", AmountCents: 1, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
	if err := s.SetLimit("research", limit); err != nil {
		t.Fatal(err)
	}

	const attempts = 100
	start := make(chan struct{})
	results := make(chan error, attempts)
	var workers sync.WaitGroup
	workers.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer workers.Done()
			<-start
			_, reserveErr := s.Reserve("research", fmt.Sprintf("concurrent-%d", index), 1, true)
			results <- reserveErr
		}(index)
	}
	close(start)
	workers.Wait()
	close(results)

	admitted := 0
	for reserveErr := range results {
		if reserveErr == nil {
			admitted++
			continue
		}
		if !errors.Is(reserveErr, ErrBudgetExceeded) {
			t.Fatalf("unexpected concurrent reservation error: %v", reserveErr)
		}
	}
	if admitted != 1 {
		t.Fatalf("admitted reservations = %d, want exactly one", admitted)
	}
}

func TestWorkspaceLimitAggregatesIndependentPrincipalAndRouteOperations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLimit("research", Limit{Currency: "USD", AmountCents: 10, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve("research", "principal-a:route-primary", 6, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve("research", "principal-b:route-fallback", 4, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve("research", "principal-a:route-new", 1, true); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("independent route reset Workspace budget: %v", err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reservations := reopened.Reservations("research")
	if len(reservations) != 2 || reservations[0].AmountCents+reservations[1].AmountCents != 10 {
		t.Fatalf("aggregated Workspace reservations = %#v", reservations)
	}
}

func TestReservationSurvivesRestartAndReusesIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Reserve("research", "restart-operation", 40, true); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reused, err := reopened.Reserve("research", "restart-operation", 999, true)
	if err != nil {
		t.Fatal(err)
	}
	if reused.AmountCents != 40 || reused.State != "reserved" || reused.WindowKey != time.Now().UTC().Format("2006-01-02") {
		t.Fatalf("restarted reservation = %#v", reused)
	}
	if len(reopened.state.Reservations) != 1 {
		t.Fatalf("restart duplicated reservation: %#v", reopened.state.Reservations)
	}
}

func TestInDoubtReservationSurvivesRestartUntilUncertainSettlement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetLimit("research", Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Reserve("research", "in-doubt-operation", 80, true); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Reserve("research", "new-operation", 30, true); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("in-doubt hold was released on restart: %v", err)
	}
	if err := reopened.Settle("research", "in-doubt-operation", 80, true); err != nil {
		t.Fatal(err)
	}
	settled, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(settled.state.Reservations) != 1 || settled.state.Reservations[0].State != "uncertain" {
		t.Fatalf("uncertain settlement was not durable: %#v", settled.state.Reservations)
	}
}

func TestClockRollbackCannotReopenAnEarlierAccountingWindow(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLimit("research", Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	current := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return current }
	if _, err := s.Reserve("research", "before-clock-rollback", 90, true); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return current.Add(-2 * time.Hour) }
	if _, err := s.Reserve("research", "after-clock-rollback", 20, true); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("clock rollback reopened prior window: %v", err)
	}
	reopened, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.WindowFloor != "2026-10-12" {
		t.Fatalf("window floor after reopen = %q", reopened.state.WindowFloor)
	}
}

func TestMonthlyWindowRollsOverWithoutSettlingThePriorMonth(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	limit := Limit{Currency: "USD", AmountCents: 100, Window: "month", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
	if err := s.SetLimit("research", limit); err != nil {
		t.Fatal(err)
	}
	firstMonth := time.Date(2026, 10, 31, 23, 59, 0, 0, time.UTC)
	s.now = func() time.Time { return firstMonth }
	first, err := s.Reserve("research", "monthly-operation", 80, true)
	if err != nil {
		t.Fatal(err)
	}
	if first.WindowKey != "2026-10" {
		t.Fatalf("first monthly window = %q", first.WindowKey)
	}
	s.now = func() time.Time { return firstMonth.Add(2 * time.Minute) }
	second, err := s.Reserve("research", "monthly-operation", 80, true)
	if err != nil {
		t.Fatal(err)
	}
	if second.WindowKey != "2026-11" {
		t.Fatalf("second monthly window = %q", second.WindowKey)
	}
	if err := s.SettleReservation("research", first.WindowKey, first.Operation, 75, false); err != nil {
		t.Fatal(err)
	}
	reservations := s.Reservations("research")
	if len(reservations) != 2 || reservations[0].State != "settled" || reservations[0].AmountCents != 75 || reservations[1].State != "reserved" || reservations[1].WindowKey != "2026-11" {
		t.Fatalf("monthly rollover reservations = %#v", reservations)
	}
}

func TestAdvancedWindowFloorSurvivesDeniedOperation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLimit("research", Limit{Currency: "USD", AmountCents: 1, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	firstDay := time.Date(2026, 10, 11, 23, 59, 0, 0, time.UTC)
	s.now = func() time.Time { return firstDay }
	if _, err := s.Reserve("research", "first", 1, true); err != nil {
		t.Fatal(err)
	}
	secondDay := firstDay.Add(2 * time.Minute)
	s.now = func() time.Time { return secondDay }
	if _, err := s.Reserve("research", "denied-after-rollover", 2, true); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("reservation error = %v, want budget denial", err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.WindowFloor != "2026-10-12" {
		t.Fatalf("persisted window floor = %q, want 2026-10-12", reopened.state.WindowFloor)
	}
}

func TestFailedWindowFloorPersistenceDoesNotPublishInMemoryWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	initial := time.Date(2026, 10, 11, 23, 59, 0, 0, time.UTC)
	s.now = func() time.Time { return initial }
	if _, err := s.Reserve("research", "initial", 1, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return initial.Add(2 * time.Minute) }
	if _, err := s.Reserve("research", "failed-floor", 1, true); err == nil {
		t.Fatal("obstructed window-floor persistence succeeded")
	}
	if s.state.WindowFloor != "2026-10-11" {
		t.Fatalf("failed persistence published floor %q", s.state.WindowFloor)
	}
}

func TestApprovedLimitAuditIsAtomicDurableAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	limit := Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
	change := LimitChange{ApprovalID: "approval", Workspace: "research", Actor: "local", Digest: "digest", Revision: s.Revision()}
	if err := s.ApplyReviewedLimitChecked(t.Context(), "research", limit, change, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	revision := s.Revision()
	if err := s.ApplyReviewedLimitChecked(t.Context(), "research", limit, change, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(s.LimitChanges("research")) != 1 || s.Revision() != revision {
		t.Fatal("replay duplicated mutation/audit")
	}
	changed := limit
	changed.AmountCents++
	if err := s.ApplyReviewedLimitChecked(t.Context(), "research", changed, change, func(context.Context) error { return nil }); err == nil {
		t.Fatal("changed replay admitted")
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reopened.Limit("research"); !ok || got != limit || len(reopened.LimitChanges("research")) != 1 {
		t.Fatal("mutation/audit not durable together")
	}
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	change.ApprovalID = "second"
	change.Revision = revision
	if err := s.ApplyReviewedLimitChecked(t.Context(), "research", changed, change, func(context.Context) error { return nil }); err == nil {
		t.Fatal("obstructed write succeeded")
	}
	if got, _ := s.Limit("research"); got != limit || len(s.LimitChanges("research")) != 1 || s.Revision() != revision {
		t.Fatal("failed write published mutation or audit")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyReviewedLimitChecked(t.Context(), "research", changed, change, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	audit := s.LimitChanges("research")
	if len(audit) != 2 || audit[1].Before == nil || *audit[1].Before != limit || audit[1].After != changed {
		t.Fatal("audit before/after mismatch")
	}
	audit[1].Before.AmountCents = 999
	if s.LimitChanges("research")[1].Before.AmountCents != limit.AmountCents {
		t.Fatal("returned audit aliases live state")
	}
}
