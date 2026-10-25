package search

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
)

type searchFunc func(context.Context, Query) ([]Result, error)

func (f searchFunc) Search(ctx context.Context, q Query) ([]Result, error) { return f(ctx, q) }

func TestSearchFallbackRunsOnlyAfterFailureAndRetainsPerAttemptEvidence(t *testing.T) {
	calls := []string{}
	events := []Evidence{}
	svc := Service{
		ConnectionIDs: func(string) ([]string, bool) { return []string{"primary", "secondary"}, true },
		Sources: map[string]Source{
			"primary": searchFunc(func(context.Context, Query) ([]Result, error) {
				calls = append(calls, "primary")
				return nil, errors.New("upstream unavailable")
			}),
			"secondary": searchFunc(func(context.Context, Query) ([]Result, error) {
				calls = append(calls, "secondary")
				return []Result{{URL: "https://example.test", Title: "found"}}, nil
			}),
		},
		Evidence: func(e Evidence) { events = append(events, e) },
	}
	got, err := svc.Search(t.Context(), "research", Query{Text: "test"})
	if err != nil || len(got) != 1 || len(calls) != 2 || len(events) != 4 || events[1].Outcome != "failed" || events[3].Outcome != "succeeded" || events[0].Operation == events[2].Operation {
		t.Fatalf("result=%+v err=%v calls=%v events=%+v", got, err, calls, events)
	}
	svc.Sources["primary"] = searchFunc(func(context.Context, Query) ([]Result, error) { calls = append(calls, "empty"); return []Result{}, nil })
	calls = nil
	got, err = svc.Search(t.Context(), "research", Query{Text: "unmatched"})
	if err != nil || len(got) != 0 || len(calls) != 1 || calls[0] != "empty" {
		t.Fatalf("empty success incorrectly fell back: result=%v err=%v calls=%v", got, err, calls)
	}
}

func TestSearchFallbackStopsOnAccountingDenialAndCancellation(t *testing.T) {
	store, err := accounting.Open(t.TempDir() + "/accounting.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetLimit("research", accounting.Limit{Currency: "USD", AmountCents: 5, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	secondCalls := 0
	svc := Service{
		ConnectionIDs: func(string) ([]string, bool) { return []string{"primary", "secondary"}, true },
		Sources: map[string]Source{
			"primary": searchFunc(func(context.Context, Query) ([]Result, error) { return nil, errors.New("failed upstream") }),
			"secondary": searchFunc(func(context.Context, Query) ([]Result, error) {
				secondCalls++
				return []Result{{URL: "https://example.test"}}, nil
			}),
		},
		Accounting: store, CostCents: 5, KnownPrice: true,
	}
	_, err = svc.Search(t.Context(), "research", Query{Text: "query"})
	if !errors.Is(err, accounting.ErrBudgetExceeded) || secondCalls != 0 {
		t.Fatalf("fallback evaded spending policy err=%v calls=%d", err, secondCalls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = svc.Search(ctx, "research", Query{Text: "query"})
	if !errors.Is(err, context.Canceled) || secondCalls != 0 {
		t.Fatalf("cancelled request retried: %v calls=%d", err, secondCalls)
	}
}

type boundSearchFixture struct {
	source   Source
	id       connectionidentity.ID
	revision connections.Revision
}

func (b boundSearchFixture) Search(ctx context.Context, q Query) ([]Result, error) {
	return b.source.Search(ctx, q)
}
func (b boundSearchFixture) ConnectionID() connectionidentity.ID      { return b.id }
func (b boundSearchFixture) ConnectionRevision() connections.Revision { return b.revision }
func revisionBoundFixtureSource(t *testing.T, rawID, phase string) Source {
	t.Helper()
	id, err := connectionidentity.Parse(rawID)
	if err != nil {
		t.Fatal(err)
	}
	version := "1"
	if phase == "new" {
		version = "2"
	}
	revision, err := connections.ParseRevision(version)
	if err != nil {
		t.Fatal(err)
	}
	return boundSearchFixture{id: id, revision: revision, source: searchFunc(func(context.Context, Query) ([]Result, error) {
		return []Result{{URL: "https://example.test", Title: phase}}, nil
	})}
}
func TestSearchSourceForCapturesFreshBindingEachAttempt(t *testing.T) {
	current := "old"
	var resolutions []string
	svc := Service{
		ConnectionIDs: func(string) ([]string, bool) { return []string{"cn_search"}, true },
		SourceFor: func(_ context.Context, id string) (Source, error) {
			resolutions = append(resolutions, id+":"+current)
			return revisionBoundFixtureSource(t, id, current), nil
		},
	}
	if _, err := svc.Search(t.Context(), "research", Query{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	current = "new"
	if _, err := svc.Search(t.Context(), "research", Query{Text: "second"}); err != nil {
		t.Fatal(err)
	}
	if len(resolutions) != 2 || resolutions[0] != "cn_search:old" || resolutions[1] != "cn_search:new" {
		t.Fatalf("stale source resolution: %v", resolutions)
	}
}

func TestSearchFallbackSkipsUnavailableBindingWithoutChargingIt(t *testing.T) {
	var events []Evidence
	attempts := 0
	svc := Service{
		ConnectionIDs: func(string) ([]string, bool) { return []string{"cn_missing", "cn_ok"}, true },
		SourceFor: func(_ context.Context, id string) (Source, error) {
			if id == "cn_missing" {
				return nil, ErrNotConfigured
			}
			return revisionBoundFixtureSource(t, id, "new"), nil
		},
		Evidence: func(e Evidence) {
			events = append(events, e)
			if e.Outcome == "started" {
				attempts++
			}
		},
	}
	got, err := svc.Search(context.Background(), "research", Query{Text: "query"})
	if err != nil || len(got) != 1 || attempts != 1 || len(events) != 2 || events[0].ConnectionID != "cn_ok" {
		t.Fatalf("unavailable source charged/executed? result=%v err=%v attempts=%d events=%v", got, err, attempts, events)
	}
}

func TestSearchFallbackDoesNotEscapeFailedSettlement(t *testing.T) {
	root := t.TempDir()
	parent := root + "/accounting"
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := accounting.Open(parent + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	secondCalls := 0
	svc := Service{
		ConnectionIDs: func(string) ([]string, bool) { return []string{"primary", "backup"}, true },
		Sources: map[string]Source{
			"primary": searchFunc(func(context.Context, Query) ([]Result, error) {
				// Simulate a real durable settlement failure after reservation.
				if err := os.RemoveAll(parent); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(parent, []byte("blocked"), 0600); err != nil {
					t.Fatal(err)
				}
				return nil, errors.New("provider failed")
			}),
			"backup": searchFunc(func(context.Context, Query) ([]Result, error) {
				secondCalls++
				return []Result{{URL: "https://backup.test"}}, nil
			}),
		},
		Accounting: store, CostCents: 1, KnownPrice: true,
	}
	_, err = svc.Search(context.Background(), "research", Query{Text: "query"})
	if err == nil || secondCalls != 0 {
		t.Fatalf("second upstream escaped failed settlement: err=%v secondCalls=%d", err, secondCalls)
	}
}

func TestSearchBindingCancellationIsTerminalBeforeAccountingOrDispatch(t *testing.T) {
	for _, bindingFails := range []bool{false, true} {
		t.Run(fmt.Sprint(bindingFails), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			calls := 0
			svc := Service{
				ConnectionIDs: func(string) ([]string, bool) { return []string{"primary"}, true },
				SourceFor: func(context.Context, string) (Source, error) {
					cancel()
					if bindingFails {
						return nil, context.Canceled
					}
					return searchFunc(func(context.Context, Query) ([]Result, error) { calls++; return nil, nil }), nil
				},
			}
			_, err := svc.Search(ctx, "research", Query{Text: "query"})
			if !errors.Is(err, context.Canceled) || calls != 0 {
				t.Fatalf("binding cancellation err=%v upstream calls=%d", err, calls)
			}
		})
	}
}

func TestSearchFallbackAccountsEachInvokedProvider(t *testing.T) {
	store, err := accounting.Open(t.TempDir() + "/accounting.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetLimit("research", accounting.Limit{Currency: "USD", AmountCents: 10, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	var events []Evidence
	svc := Service{ConnectionIDs: func(string) ([]string, bool) { return []string{"first", "second"}, true }, Sources: map[string]Source{
		"first":  searchFunc(func(context.Context, Query) ([]Result, error) { return nil, errors.New("unavailable") }),
		"second": StaticSource{{Title: "query"}},
	}, Accounting: store, CostCents: 2, KnownPrice: true, Evidence: func(e Evidence) { events = append(events, e) }}
	if _, err := svc.Search(t.Context(), "research", Query{Text: "query"}); err != nil {
		t.Fatal(err)
	}
	status := store.Status("research")
	if status.ChargedCents != "4" || status.ReservedCents != "0" || len(events) != 4 || events[0].Operation == events[2].Operation {
		t.Fatalf("per-provider accounting=%+v events=%+v", status, events)
	}
}
