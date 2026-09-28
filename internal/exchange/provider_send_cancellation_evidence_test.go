package exchange

import (
	"context"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/execution"
	evidence "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestProviderSendCancellationSettlesOneInvocationWithoutFailure(t *testing.T) {
	id, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	route, err := routing.ParseRouteName("primary")
	if err != nil {
		t.Fatal(err)
	}
	capture := &characterizationEvidenceCapture{}
	ordinal := uint64(0)
	state := exchangeState{
		input: exchangeInput{canonicalExchangeID: id}, invocationOrdinal: &ordinal,
		providerCallAttempts: []providerCallAttempt{{status: providerCallAttemptCalling}},
	}
	command := callProviderCommand{
		attemptID: 1, route: route,
		backend: provider.Backend{Target: provider.TargetSnapshot{
			TargetID: "target", TargetVersion: 1, ProviderSpec: "openai",
			Model: "model", ProtocolKind: protocolkind.Responses,
		}},
	}
	invocation, err := startModelInvocation(&state, command, capture, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	failure := provider.AttemptMayHaveExecuted(provider.Cancelled(context.Canceled))
	emitProviderInvocationTransportSettlement(capture, state, command, providerCallFailed{attemptID: command.attemptID, failure: failure})
	if ordinal != 1 || len(capture.events) != 2 {
		t.Fatalf("send cancellation: ordinal=%d facts=%#v", ordinal, capture.events)
	}
	started, ok := capture.events[0].(evidence.ModelInvocationStarted)
	if !ok || started.ID() != invocation {
		t.Fatalf("provider invocation start = %#v", capture.events[0])
	}
	settled, ok := capture.events[1].(evidence.ModelInvocationSettled)
	if !ok || settled.ID() != invocation || settled.Outcome() != execution.Cancelled ||
		settled.Occurrence() != execution.MayHaveOccurred || settled.Failure() != "" {
		t.Fatalf("provider Send cancellation terminal = %#v", capture.events[1])
	}
}
