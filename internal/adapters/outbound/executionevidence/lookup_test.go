package executionevidence

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestExactExchangeLookupDetachesActiveAndSettledEvidence(t *testing.T) {
	store := NewStore(Config{})
	started, invocation, terminal, settled := lifecycle(t, 1)
	for _, event := range []domain.Event{started, invocation} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	active, found := store.Exchange(started.ID())
	if !found || active.Settled != nil || len(active.Invocations) != 1 {
		t.Fatal("exact active Exchange lookup lost lifecycle")
	}
	active.Invocations[0] = InvocationLifecycle{}
	again, found := store.Exchange(started.ID())
	if !found || again.Invocations[0].Started == nil {
		t.Fatal("lookup returned mutable invocation storage")
	}
	for _, event := range []domain.Event{terminal, settled} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	completed, found := store.Exchange(started.ID())
	if !found || completed.Settled == nil || completed.Settled.ID() != started.ID() || completed.Invocations[0].Settled == nil {
		t.Fatal("exact settled Exchange lookup lost lifecycle")
	}
	*completed.Settled = domain.ExchangeSettled{}
	retained, found := store.Exchange(started.ID())
	if !found || retained.Settled == nil || retained.Settled.ID() != started.ID() {
		t.Fatal("lookup returned mutable settlement storage")
	}
	if _, found := store.Exchange(execution.ExchangeID{}); found {
		t.Fatal("empty Exchange identity fabricated")
	}
}

func TestExactStandaloneLookupPreservesUncertaintyAndBoundedAbsence(t *testing.T) {
	store := NewStore(Config{SettledLimit: 1})
	workspace, _ := routing.ParseWorkspaceSlug("research")
	identity, err := store.BeginMCP(accessidentity.Local, workspace, "docs", "write")
	if err != nil {
		t.Fatal(err)
	}
	active, found := store.MCPOperation(identity)
	if !found || !active.SettledAt.IsZero() || active.Outcome != "" {
		t.Fatal("active MCP lookup invented settlement")
	}
	active.Source = "changed"
	if err := store.SettleMCP(identity, execution.Failed, execution.MayHaveOccurred); err != nil {
		t.Fatal(err)
	}
	completed, found := store.MCPOperation(identity)
	if !found || completed.Source != "docs" || completed.Occurrence != execution.MayHaveOccurred || completed.SettledAt.IsZero() {
		t.Fatal("settled MCP lookup lost independent source uncertainty")
	}
	newer, err := store.BeginMCP(accessidentity.Local, workspace, "docs", "write")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SettleMCP(newer, execution.Succeeded, execution.Occurred); err != nil {
		t.Fatal(err)
	}
	if _, found := store.MCPOperation(identity); found {
		t.Fatal("evicted evidence retained outside bounded store")
	}
	if _, found := store.MCPOperation("missing"); found {
		t.Fatal("missing standalone operation fabricated")
	}
}
