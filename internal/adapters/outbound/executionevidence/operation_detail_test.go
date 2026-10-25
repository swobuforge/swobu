package executionevidence

import (
	"testing"

	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
)

func TestExchangeDetailPreservesInvocationAndDeliveryLifecycle(t *testing.T) {
	store := NewStore(Config{})
	started, invocation, terminal, settled := lifecycle(t, 1)
	for _, event := range []domain.Event{started, invocation} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	active, _ := store.Exchange(started.ID())
	detail := ProjectExchangeDetail(active, "memory")
	if detail.Kind != "exchange" || detail.MCP != nil || detail.Exchange.Identity != started.ID().String() || !detail.Exchange.Settled.IsZero() || len(detail.Exchange.Invocations) != 1 || detail.Exchange.Invocations[0].Kind != "mcp_tool" || detail.Exchange.Invocations[0].Outcome != "" {
		t.Fatal("active detail fabricated terminal evidence")
	}
	for _, event := range []domain.Event{terminal, settled} {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	completed, _ := store.Exchange(started.ID())
	detail = ProjectExchangeDetail(completed, "memory")
	if detail.Exchange.Result != string(settled.Result()) || detail.Exchange.Settled != settled.SettledAt() || detail.Exchange.Invocations[0].Outcome != terminal.Outcome() || detail.Exchange.Invocations[0].Occurrence != terminal.Occurrence() || detail.Exchange.Invocations[0].Settled != terminal.SettledAt() {
		t.Fatal("detail conflated delivery and source outcome")
	}
}
