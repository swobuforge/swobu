// Package ledgertest owns the backend-neutral Ledger Store conformance suite.
// Every production backend runs these exact fixtures; adapter-specific tests
// remain responsible only for physical database behavior.
package ledgertest

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/routing"
)

// Factory returns an isolated Store for one conformance subtest.
type Factory func(testing.TB) ledger.Store

// Run executes the portable behavior contract shared by SQLite and PostgreSQL.
func Run(t *testing.T, factory Factory) {
	t.Helper()
	t.Run("round trip mixed lifecycle and identities", func(t *testing.T) {
		store := factory(t)
		exchange := mixedExchange(t, fixtureOptions{settledAt: fixtureHour.Add(10 * time.Minute), targetVersion: math.MaxUint64, inputTokens: intPointer(0)})
		mustWrite(t, store, exchange)
		got, found, err := store.GetExchange(context.Background(), exchange.ID())
		if err != nil || !found {
			t.Fatalf("GetExchange found=%v err=%v", found, err)
		}
		assertLifecycle(t, got, exchange)
		model := got.Invocations()[0].Started().(executionevidence.ModelInvocationStarted)
		if uint64(model.Model().Version()) != math.MaxUint64 {
			t.Fatalf("TargetVersion = %d, want MaxUint64", model.Model().Version())
		}
		usage := got.Invocations()[0].Settled().(executionevidence.ModelInvocationSettled).Usage()
		if value, known := usage.InputTokens(); !known || value != 0 {
			t.Fatalf("known-zero input tokens = %d,%v", value, known)
		}
		if _, known := usage.OutputTokens(); known {
			t.Fatal("unknown output tokens became known")
		}
	})

	t.Run("zero Invocation and not found", func(t *testing.T) {
		store := factory(t)
		exchange := zeroInvocationExchange(t, fixtureHour)
		mustWrite(t, store, exchange)
		got, found, err := store.GetExchange(context.Background(), exchange.ID())
		if err != nil || !found || len(got.Invocations()) != 0 {
			t.Fatalf("zero Invocation round trip = %#v, found=%v err=%v", got, found, err)
		}
		missing, _ := execution.NewExchangeID()
		if _, found, err := store.GetExchange(context.Background(), missing); err != nil || found {
			t.Fatalf("missing Exchange found=%v err=%v", found, err)
		}
	})

	t.Run("deterministic cursor pagination", func(t *testing.T) {
		store := factory(t)
		first := zeroInvocationExchange(t, fixtureHour)
		second := zeroInvocationExchange(t, fixtureHour.Add(time.Minute))
		third := zeroInvocationExchange(t, fixtureHour.Add(2*time.Minute))
		mustWrite(t, store, first, second, third)
		page1, err := store.ListRecentSummaries(context.Background(), ledger.ExchangeFilter{}, ledger.Cursor{}, 2)
		if err != nil || len(page1.Exchanges) != 2 || !page1.Next.Valid() || page1.Exchanges[0].ID != third.ID() || page1.Exchanges[1].ID != second.ID() {
			t.Fatalf("page 1 = %#v err=%v", page1, err)
		}
		page2, err := store.ListRecentSummaries(context.Background(), ledger.ExchangeFilter{}, page1.Next, 2)
		if err != nil || len(page2.Exchanges) != 1 || page2.Next.Valid() || page2.Exchanges[0].ID != first.ID() {
			t.Fatalf("page 2 = %#v err=%v", page2, err)
		}
	})

	t.Run("Invocation predicates match one Invocation", func(t *testing.T) {
		store := factory(t)
		split := twoModelExchange(t, fixtureHour, "openai", execution.Succeeded, "anthropic", execution.Failed)
		matching := twoModelExchange(t, fixtureHour.Add(time.Minute), "openai", execution.Failed, "anthropic", execution.Succeeded)
		mustWrite(t, store, split, matching)
		provider := routing.Provider("openai")
		outcome := execution.Failed
		page, err := store.ListRecentSummaries(context.Background(), ledger.ExchangeFilter{Provider: &provider, InvocationOutcome: &outcome}, ledger.Cursor{}, 50)
		if err != nil || len(page.Exchanges) != 1 || page.Exchanges[0].ID != matching.ID() {
			t.Fatalf("same-Invocation filter = %#v err=%v", page, err)
		}
	})

	t.Run("detail retention removes expired detail", func(t *testing.T) {
		store := factory(t)
		now := fixtureHour.AddDate(0, 0, 40)
		exchange := mixedExchange(t, fixtureOptions{settledAt: fixtureHour, targetVersion: 1, inputTokens: intPointer(4)})
		mustWrite(t, store, exchange)
		err := store.Maintain(context.Background(), now)
		if err != nil {
			t.Fatalf("maintenance err=%v", err)
		}
		if _, found, err := store.GetExchange(context.Background(), exchange.ID()); err != nil || found {
			t.Fatalf("retained detail found=%v err=%v", found, err)
		}
	})
}

var fixtureHour = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

type fixtureOptions struct {
	settledAt     time.Time
	targetVersion uint64
	inputTokens   *int
}

func mixedExchange(t testing.TB, options fixtureOptions) executionevidence.CompletedExchange {
	t.Helper()
	started := exchangeStart(t, options.settledAt.Add(-4*time.Second))
	model := modelInvocation(t, started.ID(), 1, "openai", execution.Succeeded, options.targetVersion, options.inputTokens)
	mcp := mcpInvocation(t, started.ID(), 2)
	settled := exchangeSettlement(t, started.ID(), options.settledAt)
	completed, err := executionevidence.NewCompletedExchange(started, []executionevidence.InvocationLifecycle{model, mcp}, settled)
	if err != nil {
		t.Fatal(err)
	}
	return completed
}

func twoModelExchange(t testing.TB, settledAt time.Time, firstProvider string, firstOutcome execution.Outcome, secondProvider string, secondOutcome execution.Outcome) executionevidence.CompletedExchange {
	t.Helper()
	started := exchangeStart(t, settledAt.Add(-4*time.Second))
	first := modelInvocation(t, started.ID(), 1, firstProvider, firstOutcome, 1, nil)
	second := modelInvocation(t, started.ID(), 2, secondProvider, secondOutcome, 2, nil)
	completed, err := executionevidence.NewCompletedExchange(started, []executionevidence.InvocationLifecycle{first, second}, exchangeSettlement(t, started.ID(), settledAt))
	if err != nil {
		t.Fatal(err)
	}
	return completed
}

func zeroInvocationExchange(t testing.TB, settledAt time.Time) executionevidence.CompletedExchange {
	t.Helper()
	started := exchangeStart(t, settledAt.Add(-time.Second))
	completed, err := executionevidence.NewCompletedExchange(started, nil, exchangeSettlement(t, started.ID(), settledAt))
	if err != nil {
		t.Fatal(err)
	}
	return completed
}

func exchangeStart(t testing.TB, at time.Time) executionevidence.ExchangeStarted {
	t.Helper()
	id, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.ParseWorkspaceSlug("acme")
	if err != nil {
		t.Fatal(err)
	}
	started, err := executionevidence.NewExchangeStarted(id, accessidentity.Local, workspace, at, clientfamily.Codex, protocolkind.Responses, canonical.NormalizedPathResponses)
	if err != nil {
		t.Fatal(err)
	}
	return started
}

func exchangeSettlement(t testing.TB, id execution.ExchangeID, at time.Time) executionevidence.ExchangeSettled {
	t.Helper()
	settled, err := executionevidence.NewExchangeSettled(id, at, delivery.Succeeded, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	return settled
}

func modelInvocation(t testing.TB, exchangeID execution.ExchangeID, ordinal uint64, provider string, outcome execution.Outcome, version uint64, input *int) executionevidence.InvocationLifecycle {
	t.Helper()
	id, err := execution.NewInvocationID(exchangeID, ordinal)
	if err != nil {
		t.Fatal(err)
	}
	route, _ := routing.ParseRouteName("primary")
	target, _ := routing.ParseTargetID("target-" + provider)
	model, _ := routing.ParseUpstreamModel("model-" + provider)
	resource, err := executionevidence.NewModelResource(route, target, routing.TargetVersion(version), routing.Provider(provider), protocolkind.Responses, model, executionevidence.ExchangeAttempt)
	if err != nil {
		t.Fatal(err)
	}
	started, err := executionevidence.NewModelInvocationStarted(id, fixtureHour.Add(time.Duration(ordinal)*time.Second), resource)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: input})
	if err != nil {
		t.Fatal(err)
	}
	failure := executionevidence.ModelFailure("")
	occurrence := execution.Occurred
	if outcome == execution.Failed {
		failure = executionevidence.FailureTimeout
		occurrence = execution.MayHaveOccurred
	}
	settled, err := executionevidence.NewModelInvocationSettled(id, fixtureHour.Add(time.Duration(ordinal)*time.Second+time.Millisecond), outcome, occurrence, failure, usage)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := executionevidence.NewInvocationLifecycle(started, settled)
	if err != nil {
		t.Fatal(err)
	}
	return completed
}

func mcpInvocation(t testing.TB, exchangeID execution.ExchangeID, ordinal uint64) executionevidence.InvocationLifecycle {
	t.Helper()
	id, _ := execution.NewInvocationID(exchangeID, ordinal)
	tool, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "search")
	resource, _ := executionevidence.NewMCPResource(tool, tool)
	started, err := executionevidence.NewMCPInvocationStarted(id, fixtureHour.Add(2*time.Second), resource)
	if err != nil {
		t.Fatal(err)
	}
	settled, err := executionevidence.NewMCPInvocationSettled(id, fixtureHour.Add(2*time.Second+time.Millisecond), execution.Succeeded, execution.Occurred, "")
	if err != nil {
		t.Fatal(err)
	}
	completed, err := executionevidence.NewInvocationLifecycle(started, settled)
	if err != nil {
		t.Fatal(err)
	}
	return completed
}

func mustWrite(t testing.TB, store ledger.Store, exchanges ...executionevidence.CompletedExchange) {
	t.Helper()
	if err := store.WriteBatch(context.Background(), exchanges); err != nil {
		t.Fatal(err)
	}
}

func assertLifecycle(t testing.TB, got, want executionevidence.CompletedExchange) {
	t.Helper()
	if got.ID() != want.ID() || got.Duration() != want.Duration() || got.Started().AccessID() != want.Started().AccessID() || got.Started().Workspace() != want.Started().Workspace() || got.Settled().Result() != want.Settled().Result() || len(got.Invocations()) != len(want.Invocations()) {
		t.Fatalf("lifecycle mismatch: got=%#v want=%#v", got, want)
	}
	for index := range want.Invocations() {
		if got.Invocations()[index].Started().ID() != want.Invocations()[index].Started().ID() || got.Invocations()[index].Duration() != want.Invocations()[index].Duration() {
			t.Fatalf("Invocation %d mismatch", index)
		}
	}
}

func intPointer(value int) *int { return &value }
