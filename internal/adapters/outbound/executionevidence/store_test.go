package executionevidence

import (
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/execution"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/routing"
)

func lifecycle(t *testing.T, seed byte) (domain.ExchangeStarted, domain.InvocationStartedEvent, domain.InvocationSettledEvent, domain.ExchangeSettled) {
	t.Helper()
	id, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.ParseWorkspaceSlug("acme")
	if err != nil {
		t.Fatal(err)
	}
	started, err := domain.NewExchangeStarted(id, accessidentity.Local, workspace, time.Unix(int64(seed), 0), clientfamily.Codex, "responses", canonical.NormalizedPathResponses)
	if err != nil {
		t.Fatal(err)
	}
	invocationID, err := execution.NewInvocationID(id, 1)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "search")
	if err != nil {
		t.Fatal(err)
	}
	resource, err := domain.NewMCPResource(tool, tool)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := domain.NewMCPInvocationStarted(invocationID, time.Unix(int64(seed), 1), resource)
	if err != nil {
		t.Fatal(err)
	}
	settledInvocation, err := domain.NewMCPInvocationSettled(invocationID, time.Unix(int64(seed), 2), execution.Succeeded, execution.Occurred, "")
	if err != nil {
		t.Fatal(err)
	}
	settledExchange, err := domain.NewExchangeSettled(id, time.Unix(int64(seed), 3), delivery.Succeeded, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	return started, invocation, settledInvocation, settledExchange
}

func TestActivityFailedWithoutHTTPStatusUsesLifecycleState(t *testing.T) {
	store := NewStore(Config{})
	started, _, _, _ := lifecycle(t, 21)
	if err := store.Append(started); err != nil {
		t.Fatal(err)
	}
	settled, err := domain.NewExchangeSettled(started.ID(), time.Now(), delivery.ExchangeFailed, 0, canonical.ErrorCodeInternal)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(settled); err != nil {
		t.Fatal(err)
	}
	rows := store.ProjectActivity(ProjectionInput{}).Exchanges
	if len(rows) != 1 || rows[0].State != "failed" || rows[0].HTTPStatus != 0 {
		t.Fatalf("failed exchange without emitted HTTP response = %#v", rows)
	}
}

func TestStorePreservesExchangeGlobalMixedCausalOrder(t *testing.T) {
	store := NewStore(Config{})
	started, _, _, settled := lifecycle(t, 9)
	if err := store.Append(started); err != nil {
		t.Fatal(err)
	}
	target, err := routing.ParseTargetID("model-a")
	if err != nil {
		t.Fatal(err)
	}
	model, err := routing.ParseUpstreamModel("upstream-a")
	if err != nil {
		t.Fatal(err)
	}
	protocol, err := protocolkind.ParseProtocolKind("responses")
	if err != nil {
		t.Fatal(err)
	}
	route, err := routing.ParseRouteName("primary")
	if err != nil {
		t.Fatal(err)
	}
	resource, err := domain.NewModelResource(route, target, 3, routing.Provider("openai"), protocol, model, domain.ExchangeAttempt)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]execution.InvocationID, 0, 3)
	for i := 0; i < 3; i++ {
		id, err := execution.NewInvocationID(started.ID(), uint64(i+1))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		var invocation domain.InvocationStartedEvent
		if i == 1 {
			tool, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "search")
			mcp, _ := domain.NewMCPResource(tool, tool)
			invocation, err = domain.NewMCPInvocationStarted(id, time.Unix(10+int64(i), 0), mcp)
		} else {
			invocation, err = domain.NewModelInvocationStarted(id, time.Unix(10+int64(i), 0), resource)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Append(invocation); err != nil {
			t.Fatal(err)
		}
		var settlement domain.InvocationSettledEvent
		if i == 1 {
			settlement, err = domain.NewMCPInvocationSettled(id, time.Unix(20+int64(i), 0), execution.Succeeded, execution.Occurred, "")
		} else {
			settlement, err = domain.NewModelInvocationSettled(id, time.Unix(20+int64(i), 0), execution.Succeeded, execution.Occurred, "", canonical.NewUnknownTokenUsage())
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Append(settlement); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Append(settled); err != nil {
		t.Fatal(err)
	}
	history := store.Settled()
	if len(history) != 1 || len(history[0].Invocations) != 3 {
		t.Fatalf("mixed lifecycle = %#v", history)
	}
	for i, invocation := range history[0].Invocations {
		if invocation.Started.Ordinal() != uint64(i+1) || invocation.Started.ID() != ids[i] || invocation.Settled == nil {
			t.Fatalf("invocation %d order = %#v", i, invocation)
		}
	}
	if _, ok := history[0].Invocations[1].Started.(domain.MCPInvocationStarted); !ok {
		t.Fatal("middle invocation lost MCP resource kind")
	}
}

func TestStoreGroupsLifecycleAndEvictsWholeSettledExchanges(t *testing.T) {
	store := NewStore(Config{SettledLimit: 1})
	first, invocation, invocationSettled, firstSettled := lifecycle(t, 1)
	if err := store.Append(first); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(invocation); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(invocationSettled); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(firstSettled); err != nil {
		t.Fatal(err)
	}
	if len(store.Active()) != 0 || len(store.Settled()) != 1 || len(store.Settled()[0].Invocations) != 1 {
		t.Fatalf("first lifecycle retention = active=%d settled=%d invocations=%d", len(store.Active()), len(store.Settled()), len(store.Settled()[0].Invocations))
	}
	second, secondInvocation, secondInvocationSettled, secondSettled := lifecycle(t, 2)
	if err := store.Append(second); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(secondInvocation); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(secondInvocationSettled); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(secondSettled); err != nil {
		t.Fatal(err)
	}
	history := store.Settled()
	if len(history) != 1 || history[0].Started.StartedAt() != second.StartedAt() {
		t.Fatalf("whole exchange eviction failed: %#v", history)
	}
}

func TestStoreRejectsIncompleteOrDuplicateLifecycleTransitions(t *testing.T) {
	store := NewStore(Config{})
	started, invocation, invocationSettled, settled := lifecycle(t, 3)
	if err := store.Append(invocation); err == nil {
		t.Fatal("invocation without exchange accepted")
	}
	if err := store.Append(started); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(started); err == nil {
		t.Fatal("duplicate exchange accepted")
	}
	if err := store.Append(invocationSettled); err == nil {
		t.Fatal("settlement without invocation accepted")
	}
	if err := store.Append(invocation); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(invocationSettled); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(invocationSettled); err == nil {
		t.Fatal("duplicate invocation settlement accepted")
	}
	if err := store.Append(settled); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsOutOfOrderInvocationOrdinal(t *testing.T) {
	store := NewStore(Config{})
	started, _, _, _ := lifecycle(t, 11)
	if err := store.Append(started); err != nil {
		t.Fatal(err)
	}
	secondID, err := execution.NewInvocationID(started.ID(), 2)
	if err != nil {
		t.Fatal(err)
	}
	tool, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "search")
	resource, _ := domain.NewMCPResource(tool, tool)
	invocation, err := domain.NewMCPInvocationStarted(secondID, time.Now(), resource)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(invocation); err == nil {
		t.Fatal("store accepted a skipped invocation ordinal")
	}
}

func TestStoreRetainsAllActiveExchangesBeyondSettledLimit(t *testing.T) {
	store := NewStore(Config{SettledLimit: 2})
	const active = 1000
	starts := make([]domain.ExchangeStarted, 0, active)
	for i := 0; i < active; i++ {
		started, _, _, _ := lifecycle(t, byte(i%200+1))
		// lifecycle identities are random; only retain the exchange start for
		// this capacity qualification.
		if err := store.Append(started); err != nil {
			t.Fatal(err)
		}
		starts = append(starts, started)
	}
	if got := len(store.Active()); got != active {
		t.Fatalf("active exchanges = %d, want %d", got, active)
	}
}

func TestActivityKeepsActiveExchangesNewestFirstAcrossSettledEviction(t *testing.T) {
	store := NewStore(Config{SettledLimit: 1})
	first, _, _, firstSettled := lifecycle(t, 1)
	active, _, _, _ := lifecycle(t, 2)
	latest, _, _, latestSettled := lifecycle(t, 3)
	for _, event := range []domain.Event{first, firstSettled, active, latest, latestSettled} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	rows := store.ProjectActivity(ProjectionInput{}).Exchanges
	if len(rows) != 2 || rows[0].ExchangeID != latest.ID().String() ||
		rows[1].ExchangeID != active.ID().String() || rows[1].State != "in_progress" {
		t.Fatalf("activity after settled eviction = %#v", rows)
	}
	if rows[0].ExchangeID == first.ID().String() || rows[1].ExchangeID == first.ID().String() {
		t.Fatal("evicted settled exchange remained visible in Activity")
	}
}

func TestStore1000ConcurrentActiveExchangesSettleWithoutOrphans(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent active lifecycle qualification stress")
	}
	const count = 1000
	store := NewStore(Config{SettledLimit: count})
	starts := make([]domain.ExchangeStarted, count)
	settlements := make([]domain.ExchangeSettled, count)
	for i := range starts {
		starts[i], _, _, settlements[i] = lifecycle(t, byte(i%200+1))
	}
	var workers sync.WaitGroup
	for i := range starts {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			if err := store.Append(starts[i]); err != nil {
				t.Errorf("start %d: %v", i, err)
			}
		}(i)
	}
	workers.Wait()
	if got := len(store.Active()); got != count {
		t.Fatalf("active exchanges = %d, want %d", got, count)
	}
	for i := range settlements {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			if err := store.Append(settlements[i]); err != nil {
				t.Errorf("settle %d: %v", i, err)
			}
		}(i)
	}
	workers.Wait()
	if active, settled := len(store.Active()), len(store.Settled()); active != 0 || settled != count {
		t.Fatalf("active = %d, settled = %d; want 0 and %d", active, settled, count)
	}
}

func TestStoreConcurrentAppendAndSnapshot(t *testing.T) {
	store := NewStore(Config{SettledLimit: 64})
	const exchanges = 64
	starts := make([]domain.ExchangeStarted, exchanges)
	invocations := make([]domain.InvocationStartedEvent, exchanges)
	settlements := make([]domain.InvocationSettledEvent, exchanges)
	exchangeSettlements := make([]domain.ExchangeSettled, exchanges)
	for i := range starts {
		starts[i], invocations[i], settlements[i], exchangeSettlements[i] = lifecycle(t, byte(i%200+1))
	}
	var wg sync.WaitGroup
	for i := range starts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := store.Append(starts[i]); err != nil {
				t.Errorf("start %d: %v", i, err)
				return
			}
			if err := store.Append(invocations[i]); err != nil {
				t.Errorf("invocation %d: %v", i, err)
				return
			}
			if err := store.Append(settlements[i]); err != nil {
				t.Errorf("invocation settlement %d: %v", i, err)
				return
			}
			if err := store.Append(exchangeSettlements[i]); err != nil {
				t.Errorf("exchange settlement %d: %v", i, err)
			}
		}(i)
	}
	for i := 0; i < 100; i++ {
		_ = store.Active()
		_ = store.Settled()
	}
	wg.Wait()
	if got := len(store.Settled()); got != exchanges {
		t.Fatalf("settled exchanges = %d, want %d", got, exchanges)
	}
}

func TestStoreBoundsManagedAdmissionRetention(t *testing.T) {
	store := NewStore(Config{AdmissionLimit: 2})
	workspace, err := routing.ParseWorkspaceSlug("alpha")
	if err != nil {
		t.Fatal(err)
	}
	for i, reason := range []domain.AdmissionReason{domain.InvalidCredential, domain.AmbiguousCredentials, domain.InvalidCredential} {
		event, err := domain.NewManagedAccessAdmissionRejected(time.Unix(int64(i+1), 0), workspace, accessidentity.ID{}, reason)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	admissions := store.Admissions()
	if len(admissions) != 2 || admissions[0].ObservedAt() != time.Unix(2, 0) || admissions[1].ObservedAt() != time.Unix(3, 0) {
		t.Fatalf("admission retention = %#v", admissions)
	}
	for _, admission := range admissions {
		if admission.AccessID().Valid() {
			t.Fatal("coarse admission unexpectedly retained an access identity")
		}
	}
}

func TestStore100KSequentialLifecycleOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("lifecycle qualification stress")
	}
	store := NewStore(Config{SettledLimit: 128})
	const exchanges = 100_000
	for i := 0; i < exchanges; i++ {
		started, invocation, invocationSettled, settled := lifecycle(t, byte(i%200+1))
		if err := store.Append(started); err != nil {
			t.Fatal(err)
		}
		if err := store.Append(invocation); err != nil {
			t.Fatal(err)
		}
		if err := store.Append(invocationSettled); err != nil {
			t.Fatal(err)
		}
		if err := store.Append(settled); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(store.Settled()); got != 128 {
		t.Fatalf("settled retention after stress = %d, want 128", got)
	}
}
