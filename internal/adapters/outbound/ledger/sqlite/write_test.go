package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/routing"
)

func completedWithoutInvocations(t *testing.T, settledAt time.Time, result delivery.ResultKind) executionevidence.CompletedExchange {
	t.Helper()
	id, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.ParseWorkspaceSlug("acme")
	if err != nil {
		t.Fatal(err)
	}
	started, err := executionevidence.NewExchangeStarted(id, accessidentity.Local, workspace, settledAt.Add(-time.Second), clientfamily.Codex, protocolkind.Responses, canonical.NormalizedPathResponses)
	if err != nil {
		t.Fatal(err)
	}
	settled, err := executionevidence.NewExchangeSettled(id, settledAt, result, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	completed, err := executionevidence.NewCompletedExchange(started, nil, settled)
	if err != nil {
		t.Fatal(err)
	}
	return completed
}

func TestWriteBatchIsAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exchange := completedWithoutInvocations(t, time.Date(2026, 9, 28, 12, 3, 0, 0, time.UTC), delivery.Succeeded)
	if err := db.WriteBatch(ctx, []executionevidence.CompletedExchange{exchange}); err != nil {
		t.Fatal(err)
	}
	if err := db.WriteBatch(ctx, []executionevidence.CompletedExchange{exchange}); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	for query, want := range map[string]int{
		`SELECT count(*) FROM swobu_ledger_exchanges`: 1,
	} {
		var got int
		if err := db.writer.QueryRowContext(ctx, query).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %d, %v; want %d", query, got, err, want)
		}
	}
}

func TestWriteBatchRejectsConflictingDuplicateWithoutChangingAggregate(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	original := completedWithoutInvocations(t, time.Date(2026, 9, 28, 12, 3, 0, 0, time.UTC), delivery.Succeeded)
	if err := db.WriteBatch(ctx, []executionevidence.CompletedExchange{original}); err != nil {
		t.Fatal(err)
	}
	conflictingSettlement, err := executionevidence.NewExchangeSettled(original.ID(), original.Settled().SettledAt(), delivery.ClientWriteFailed, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	conflicting, err := executionevidence.NewCompletedExchange(original.Started(), nil, conflictingSettlement)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WriteBatch(ctx, []executionevidence.CompletedExchange{conflicting}); err != nil {
		t.Fatal(err)
	}
}

func TestWriteBatchRejectsConflictingInvocationWithSameExchangeRowAndCount(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	original := completedModelExchange(t, executionevidence.FailureTimeout)
	if err := db.WriteBatch(ctx, []executionevidence.CompletedExchange{original}); err != nil {
		t.Fatal(err)
	}
	conflicting := completedModelExchangeWithStart(t, original.Started(), original.Settled(), executionevidence.FailureUnavailable)
	if err := db.WriteBatch(ctx, []executionevidence.CompletedExchange{conflicting}); err != nil {
		t.Fatal(err)
	}
	var invocations int
	if err := db.writer.QueryRowContext(ctx, `SELECT count(*) FROM swobu_ledger_invocations`).Scan(&invocations); err != nil {
		t.Fatal(err)
	}
	if invocations != 1 {
		t.Fatalf("conflicting retry mutated state: invocations=%d", invocations)
	}
}

func completedModelExchange(t *testing.T, failure executionevidence.ModelFailure) executionevidence.CompletedExchange {
	t.Helper()
	base := completedWithoutInvocations(t, time.Date(2026, 9, 28, 12, 3, 0, 0, time.UTC), delivery.Succeeded)
	return completedModelExchangeWithStart(t, base.Started(), base.Settled(), failure)
}

func completedModelExchangeWithStart(t *testing.T, exchangeStart executionevidence.ExchangeStarted, exchangeSettled executionevidence.ExchangeSettled, failure executionevidence.ModelFailure) executionevidence.CompletedExchange {
	t.Helper()
	id, err := execution.NewInvocationID(exchangeStart.ID(), 1)
	if err != nil {
		t.Fatal(err)
	}
	route, _ := routing.ParseRouteName("primary")
	target, _ := routing.ParseTargetID("target")
	model, _ := routing.ParseUpstreamModel("model")
	resource, err := executionevidence.NewModelResource(route, target, 1, routing.Provider("openai"), protocolkind.Responses, model, executionevidence.ExchangeAttempt)
	if err != nil {
		t.Fatal(err)
	}
	connectionID, _ := connectionidentity.Parse("cn_retained")
	resource, err = resource.BindConnection(connectionID, ^uint64(0))
	if err != nil {
		t.Fatal(err)
	}
	started, err := executionevidence.NewModelInvocationStarted(id, exchangeStart.StartedAt().Add(time.Millisecond), resource)
	if err != nil {
		t.Fatal(err)
	}
	settled, err := executionevidence.NewModelInvocationSettled(id, exchangeStart.StartedAt().Add(2*time.Millisecond), execution.Failed, execution.MayHaveOccurred, failure, canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := executionevidence.NewInvocationLifecycle(started, settled)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := executionevidence.NewCompletedExchange(exchangeStart, []executionevidence.InvocationLifecycle{invocation}, exchangeSettled)
	if err != nil {
		t.Fatal(err)
	}
	return completed
}

func TestWriteBatchRollsBackWholeLifecycleOnSubtypeFailure(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// A trigger supplies deterministic boundary fault injection without adding
	// a production chaos abstraction.
	if _, err := db.writer.ExecContext(ctx, `CREATE TRIGGER reject_exchange BEFORE INSERT ON swobu_ledger_invocations BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	exchange := completedModelExchange(t, executionevidence.FailureTimeout)
	if err := db.WriteBatch(ctx, []executionevidence.CompletedExchange{exchange}); err == nil {
		t.Fatal("injected aggregate failure was ignored")
	}
	var count int
	if err := db.writer.QueryRowContext(ctx, `SELECT count(*) FROM swobu_ledger_exchanges`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial lifecycle committed: count=%d err=%v", count, err)
	}
}

func TestConnectionEvidenceSurvivesReopen(t *testing.T) {
	cfg := testConfig(t)
	db, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	exchange := completedModelExchange(t, executionevidence.FailureTimeout)
	if err := db.WriteBatch(t.Context(), []executionevidence.CompletedExchange{exchange}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	actual, found, err := reopened.GetExchange(t.Context(), exchange.ID())
	if err != nil || !found {
		t.Fatal("retained evidence missing", err)
	}
	model := actual.Invocations()[0].Started().(executionevidence.ModelInvocationStarted).Model()
	id, revision, bound := model.ConnectionIdentity()
	if !bound || id.String() != "cn_retained" || revision != ^uint64(0) || model.Version() != 1 {
		t.Fatal("reopen lost composite identity")
	}
}
