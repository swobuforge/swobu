package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
)

// Only tests use an in-memory source with a Workspace-like identity. Runtime
// selection never falls back to a Workspace name when the resolver is missing.
func fixtureSourceForWorkspace(workspace string) func(string) ([]string, bool) {
	return func(candidate string) ([]string, bool) {
		return []string{workspace}, candidate == workspace
	}
}

type revisionBoundSource struct {
	id       connectionidentity.ID
	revision connections.Revision
}

func (s revisionBoundSource) Search(context.Context, Query) ([]Result, error) {
	return []Result{{URL: "https://example.test", Title: "result"}}, nil
}
func (s revisionBoundSource) ConnectionID() connectionidentity.ID      { return s.id }
func (s revisionBoundSource) ConnectionRevision() connections.Revision { return s.revision }

func TestSearchDoesNotSelectUpstreamWithoutWorkspaceBinding(t *testing.T) {
	svc := Service{Sources: map[string]Source{"research": StaticSource{{Title: "Go"}}}}
	if _, err := svc.Search(context.Background(), "research", Query{Text: "go"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("implicit Workspace-as-source fallback is not allowed: %v", err)
	}
}

func TestSearchEvidenceCarriesDirectConnectionRevision(t *testing.T) {
	id, err := connectionidentity.Parse("cn_search")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := connections.ParseRevision("3")
	if err != nil {
		t.Fatal(err)
	}
	var events []Evidence
	svc := Service{Sources: map[string]Source{"cn_search": revisionBoundSource{id: id, revision: revision}}, ConnectionIDs: func(string) ([]string, bool) { return []string{id.String()}, true }, Evidence: func(event Evidence) { events = append(events, event) }}
	if _, err := svc.Search(context.Background(), "research", Query{Text: "result"}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].ConnectionID != id.String() || events[0].ConnectionRevision != "3" || events[1].ConnectionRevision != "3" {
		t.Fatalf("events=%#v", events)
	}
}

func TestSearchRejectsUnversionedConnectionSource(t *testing.T) {
	svc := Service{Sources: map[string]Source{"cn_search": StaticSource{{Title: "result"}}}, ConnectionIDs: func(string) ([]string, bool) { return []string{"cn_search"}, true }}
	if _, err := svc.Search(context.Background(), "research", Query{Text: "result"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unversioned Connection source error=%v", err)
	}
}

func TestQualifiedJSONSearchRoundTripsAccountingAndEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"url":"https://example.test/result","title":"Swobu","content":"ontology"}]}`))
	}))
	defer server.Close()
	source, err := NewJSONSource(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	accountingStore, err := accounting.Open(t.TempDir() + "/accounting.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := accountingStore.SetLimit("research", accounting.Limit{Currency: "USD", AmountCents: 5, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	store := NewEvidenceStore(8)
	svc := Service{Sources: map[string]Source{"docs": source}, ConnectionIDs: func(string) ([]string, bool) { return []string{"docs"}, true }, Accounting: accountingStore, CostCents: 5, KnownPrice: true, Evidence: store.Append}
	results, err := svc.Search(context.Background(), "research", Query{Text: "swobu", Limit: 1})
	if err != nil || len(results) != 1 || results[0].Source == "" {
		t.Fatalf("results=%#v err=%v", results, err)
	}
	events := store.Events()
	if len(events) != 2 || events[0].Outcome != "started" || events[1].Outcome != "succeeded" || events[1].Uncertain {
		t.Fatalf("events=%#v", events)
	}
}

func TestSearchEmitsContentFreeLifecycleEvidence(t *testing.T) {
	var events []Evidence
	svc := Service{Sources: map[string]Source{"research": StaticSource{{Title: "Go", Snippet: "language", URL: "https://go.dev"}}}, ConnectionIDs: fixtureSourceForWorkspace("research"), Evidence: func(e Evidence) { events = append(events, e) }}
	if _, err := svc.Search(context.Background(), "research", Query{Text: "go"}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Outcome != "started" || events[1].Outcome != "succeeded" || events[0].Operation == "" || events[0].Source != "research" || events[1].EndedAt.IsZero() {
		t.Fatalf("events = %#v", events)
	}
}

func TestEvidenceStoreIsBoundedAndClonesEvents(t *testing.T) {
	store := NewEvidenceStore(1)
	store.Append(Evidence{Operation: "one"})
	store.Append(Evidence{Operation: "two"})
	events := store.Events()
	if len(events) != 1 || events[0].Operation != "two" {
		t.Fatalf("events = %#v", events)
	}
	events[0].Operation = "mutated"
	if got := store.Events()[0].Operation; got != "two" {
		t.Fatalf("store leaked mutable event: %q", got)
	}
}

func TestStaticSearchIsBoundedAndWorkspaceScoped(t *testing.T) {
	svc := Service{Sources: map[string]Source{"research": StaticSource{{Title: "Go", Snippet: "language", URL: "https://go.dev"}}}, ConnectionIDs: fixtureSourceForWorkspace("research")}
	results, err := svc.Search(context.Background(), "research", Query{Text: "go"})
	if err != nil || len(results) != 1 {
		t.Fatalf("results=%#v err=%v", results, err)
	}
	if _, err := svc.Search(context.Background(), "coding", Query{Text: "go"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err=%v", err)
	}
	if _, err := svc.Search(context.Background(), "research", Query{Text: ""}); err == nil {
		t.Fatal("empty query accepted")
	}
}

func TestSearchOnlyServiceDoesNotImplyLLMOperation(t *testing.T) {
	var events []Evidence
	svc := Service{
		Sources:       map[string]Source{"research": StaticSource{{Title: "Go", Snippet: "language", URL: "https://go.dev"}}},
		ConnectionIDs: fixtureSourceForWorkspace("research"),
		Evidence:      func(event Evidence) { events = append(events, event) },
	}
	results, err := svc.Search(context.Background(), "research", Query{Text: "go"})
	if err != nil || len(results) != 1 {
		t.Fatalf("search-only results=%#v err=%v", results, err)
	}
	if len(events) != 2 || events[0].Operation == "" || events[1].Outcome != "succeeded" {
		t.Fatalf("search-only lifecycle=%#v", events)
	}
	if _, err := (Service{}).Search(context.Background(), "research", Query{Text: "go"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("LLM-like empty service error=%v, want operation-not-configured search seam", err)
	}
}

func TestSearchAccountingReservesAndSettles(t *testing.T) {
	store, err := accounting.Open(t.TempDir() + "/accounting.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetLimit("research", accounting.Limit{Currency: "USD", AmountCents: 10, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	svc := Service{Sources: map[string]Source{"research": StaticSource{{Title: "Go", Snippet: "language", URL: "https://go.dev"}}}, ConnectionIDs: fixtureSourceForWorkspace("research"), Accounting: store, CostCents: 5, KnownPrice: true}
	if _, err := svc.Search(context.Background(), "research", Query{Text: "go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Search(context.Background(), "research", Query{Text: "go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Search(context.Background(), "research", Query{Text: "go"}); !errors.Is(err, accounting.ErrBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestSearchAdmissionDenialEmitsNoLifecycleStart(t *testing.T) {
	store, err := accounting.Open(t.TempDir() + "/accounting.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetLimit("research", accounting.Limit{Currency: "USD", AmountCents: 1, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	var events []Evidence
	svc := Service{Sources: map[string]Source{"research": StaticSource{}}, ConnectionIDs: fixtureSourceForWorkspace("research"), Accounting: store, CostCents: 2, KnownPrice: true, Evidence: func(e Evidence) { events = append(events, e) }}
	if _, err := svc.Search(context.Background(), "research", Query{Text: "go"}); !errors.Is(err, accounting.ErrBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
	if len(events) != 0 {
		t.Fatalf("admission-denied events = %#v", events)
	}
}
