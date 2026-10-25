package executionevidence

import (
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/routing"
)

// Recovery is a projection of source evidence, not a new admission or retry
// policy. Delivery must not erase an unknown upstream execution possibility.
func TestRecoveryProjectionPreservesMixedObservedAndUnresolvedExecution(t *testing.T) {
	started, _, _, _ := lifecycle(t, 42)
	route, _ := routing.ParseRouteName("coding")
	target, _ := routing.ParseTargetID("azure-east")
	model, _ := routing.ParseUpstreamModel("gpt-5")
	protocol, _ := protocolkind.ParseProtocolKind("responses")
	resource, err := domain.NewModelResource(route, target, 3, routing.Provider("openai"), protocol, model, domain.ExchangeAttempt)
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := connectionidentity.Parse("cn_account")
	resource, err = resource.BindConnection(connection, 8)
	if err != nil {
		t.Fatal(err)
	}
	firstID, _ := execution.NewInvocationID(started.ID(), 1)
	first, err := domain.NewModelInvocationStarted(firstID, started.StartedAt().Add(time.Second), resource)
	if err != nil {
		t.Fatal(err)
	}
	firstDone, err := domain.NewModelInvocationSettled(firstID, started.StartedAt().Add(2*time.Second), execution.Failed, execution.MayHaveOccurred, domain.FailureTimeout, canonical.TokenUsage{})
	if err != nil {
		t.Fatal(err)
	}
	secondID, _ := execution.NewInvocationID(started.ID(), 2)
	key, _ := canonical.NewToolKey("github", canonical.ToolKindMCP, "create_issue")
	mcp, err := domain.NewMCPResource(key, key)
	if err != nil {
		t.Fatal(err)
	}
	mcp, err = mcp.BindConnection(connection, 9)
	if err != nil {
		t.Fatal(err)
	}
	second, err := domain.NewMCPInvocationStarted(secondID, started.StartedAt().Add(3*time.Second), mcp)
	if err != nil {
		t.Fatal(err)
	}
	secondDone, err := domain.NewMCPInvocationSettled(secondID, started.StartedAt().Add(4*time.Second), execution.Succeeded, execution.Occurred, "")
	if err != nil {
		t.Fatal(err)
	}
	client, err := domain.NewExchangeSettled(started.ID(), started.StartedAt().Add(5*time.Second), delivery.ClientWriteFailed, 0, canonical.ErrorCodeInternal)
	if err != nil {
		t.Fatal(err)
	}

	lifecycle := ExchangeLifecycle{
		Started:     started,
		Invocations: []InvocationLifecycle{{Started: first, Settled: firstDone}, {Started: second, Settled: secondDone}},
		Settled:     &client,
	}
	detail := ProjectExchangeDetail(lifecycle, "ledger")
	got := detail.Exchange
	if got == nil || got.ErrorCode != string(canonical.ErrorCodeInternal) || got.Result != string(delivery.ClientWriteFailed) {
		t.Fatalf("delivery and canonical error lost: %#v", got)
	}
	if got.Upstream.Recorded != 2 || got.Upstream.Occurred != 1 || got.Upstream.MayHaveOccurred != 1 || got.Upstream.DidNotOccur != 0 || got.Upstream.Unsettled != 0 {
		t.Fatalf("unknown execution was hidden by a successful different invocation: %#v", got.Upstream)
	}
	if got.Invocations[0].Route != "coding" || got.Invocations[0].ConnectionID != "cn_account" || got.Invocations[0].ConnectionRevision != 8 || got.Invocations[1].ConnectionID != "cn_account" || got.Invocations[1].ConnectionRevision != 9 {
		t.Fatalf("can't navigate to exact source configuration revision: %#v", got.Invocations)
	}
}

func TestRecoveryProjectionNeverConvertsMissingObservationsIntoNoExecution(t *testing.T) {
	started, _, _, _ := lifecycle(t, 43)
	none, err := domain.NewExchangeSettled(started.ID(), started.StartedAt().Add(time.Second), delivery.ExchangeFailed, 503, canonical.ErrorCodeProviderUnavailable)
	if err != nil {
		t.Fatal(err)
	}
	detail := ProjectExchangeDetail(ExchangeLifecycle{Started: started, Settled: &none}, "memory").Exchange
	if detail.Upstream.Recorded != 0 || detail.Upstream.Occurred != 0 || detail.Upstream.DidNotOccur != 0 || detail.ErrorCode != string(canonical.ErrorCodeProviderUnavailable) {
		t.Fatalf("no evidence must not become evidence of no execution: %#v", detail)
	}
}

func TestRecoveryProjectionKeepsActiveInvocationUnsettled(t *testing.T) {
	started, invocation, _, _ := lifecycle(t, 44)
	detail := ProjectExchangeDetail(ExchangeLifecycle{Started: started, Invocations: []InvocationLifecycle{{Started: invocation}}}, "memory").Exchange
	if detail.Upstream.Recorded != 1 || detail.Upstream.Unsettled != 1 || detail.Upstream.Occurred != 0 || detail.Result != "" || detail.ErrorCode != "" {
		t.Fatalf("unsettled observation gained an invented terminal: %#v", detail)
	}
}
