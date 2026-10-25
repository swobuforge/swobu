package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/accounting"
)

type sourceFunc func(context.Context, Query) ([]Result, error)

func (f sourceFunc) Search(ctx context.Context, query Query) ([]Result, error) {
	return f(ctx, query)
}

func TestSearchAlwaysEmitsTerminalEvidenceAfterUpstream(t *testing.T) {
	providerFailure := errors.New("synthetic provider failure")
	for _, withAccounting := range []bool{false, true} {
		for _, providerFails := range []bool{false, true} {
			for _, settlementFails := range []bool{false, true} {
				if settlementFails && !withAccounting {
					continue
				}
				name := "without-accounting"
				if withAccounting {
					name = "with-accounting"
				}
				if providerFails {
					name += "/provider-fails"
				} else {
					name += "/provider-succeeds"
				}
				if settlementFails {
					name += "/settlement-fails"
				}
				t.Run(name, func(t *testing.T) {
					var store *accounting.Store
					parent := filepath.Join(t.TempDir(), "accounting")
					if withAccounting {
						if err := os.Mkdir(parent, 0700); err != nil {
							t.Fatal(err)
						}
						var err error
						store, err = accounting.Open(filepath.Join(parent, "state.json"))
						if err != nil {
							t.Fatal(err)
						}
					}
					calls := 0
					upstream := sourceFunc(func(context.Context, Query) ([]Result, error) {
						calls++
						// Reservation has committed. Replacing its parent with a file
						// now forces the real settlement publication to fail, even as root.
						if settlementFails {
							if err := os.RemoveAll(parent); err != nil {
								t.Fatal(err)
							}
							if err := os.WriteFile(parent, []byte("blocked"), 0600); err != nil {
								t.Fatal(err)
							}
						}
						if providerFails {
							return nil, providerFailure
						}
						return []Result{{Title: "result"}}, nil
					})
					var events []Evidence
					svc := Service{
						Sources:       map[string]Source{"docs": upstream},
						ConnectionIDs: func(string) ([]string, bool) { return []string{"docs"}, true },
						Accounting:    store, KnownPrice: true, CostCents: 1,
						Evidence: func(event Evidence) { events = append(events, event) },
					}
					results, err := svc.Search(t.Context(), "research", Query{Text: "result"})
					failed := providerFails || settlementFails
					if (err != nil) != failed || (providerFails && !errors.Is(err, providerFailure)) {
						t.Fatalf("error=%v, provider failure=%v, settlement failure=%v", err, providerFails, settlementFails)
					}
					if calls != 1 || len(events) != 2 || events[0].Outcome != "started" {
						t.Fatalf("calls=%d events=%+v", calls, events)
					}
					terminal := events[1]
					outcome := "succeeded"
					if failed {
						outcome = "failed"
					}
					if terminal.Outcome != outcome || terminal.Uncertain != failed || terminal.EndedAt.IsZero() || terminal.Operation != events[0].Operation || terminal.StartedAt != events[0].StartedAt {
						t.Fatalf("terminal=%+v", terminal)
					}
					if settlementFails && !providerFails && results != nil {
						t.Fatal("settlement failure returned successful results")
					}
				})
			}
		}
	}
}
