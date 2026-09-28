package exchange

import (
	"context"
	"errors"
	"testing"
	"time"

	store "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/execution"
	evidence "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

// This exercises the three source owners against the same exchange-local
// allocator, including a fixture which never crossed the dispatch boundary.
func TestMixedInvocationProducersShareOrdinalWithoutNotDispatchedGap(t *testing.T) {
	exchangeID, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	route, err := routing.ParseRouteName("primary")
	if err != nil {
		t.Fatal(err)
	}
	target := provider.TargetSnapshot{
		TargetID: "model-target", TargetVersion: 1, ProviderSpec: "openai",
		Model: "upstream-model", ProtocolKind: protocolkind.Responses,
	}
	var ordinal uint64
	capture := &characterizationEvidenceCapture{}
	state := exchangeState{
		input:             exchangeInput{canonicalExchangeID: exchangeID},
		invocationOrdinal: &ordinal,
		providerCallAttempts: []providerCallAttempt{
			{status: providerCallAttemptCalling},
			{status: providerCallAttemptCalling},
		},
	}
	providerCommand := func(id providerCallAttemptID) callProviderCommand {
		return callProviderCommand{attemptID: id, route: route, backend: provider.Backend{Target: target}}
	}
	first, err := startModelInvocation(&state, providerCommand(1), capture, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	settleModel := func(id execution.InvocationID) {
		t.Helper()
		settled, err := evidence.NewModelInvocationSettled(id, time.Now(), execution.Succeeded, execution.Occurred, "", canonical.NewUnknownTokenUsage())
		if err != nil {
			t.Fatal(err)
		}
		if err := capture.AppendExecutionEvidence(settled); err != nil {
			t.Fatal(err)
		}
	}
	settleModel(first)
	fixtureCalls := 0
	transport := &characterizationTransport{
		target: target, route: route, sink: capture,
		delegate: provider.TransportFunc(func(context.Context, carrier.Document) (provider.Ingress, error) {
			fixtureCalls++
			if fixtureCalls == 1 {
				return nil, provider.AttemptNotDispatched(errors.New("local preparation rejected"))
			}
			return provider.DocumentIngress{Document: carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{})}, nil
		}),
		nextInvocation: func() execution.InvocationID {
			ordinal++
			id, _ := execution.NewInvocationID(exchangeID, ordinal)
			return id
		},
	}
	request := carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{})
	if _, err := transport.Send(context.Background(), request); err == nil || ordinal != 1 {
		t.Fatalf("not-dispatched fixture: err=%v ordinal=%d", err, ordinal)
	}
	if _, err := transport.Send(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	transport.FixtureSettled(nil)
	tool, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "search")
	if err != nil {
		t.Fatal(err)
	}
	mcpResource, err := evidence.NewMCPResource(tool, tool)
	if err != nil {
		t.Fatal(err)
	}
	ordinal++
	mcpID, err := execution.NewInvocationID(exchangeID, ordinal)
	if err != nil {
		t.Fatal(err)
	}
	emitMCPInvocationStarted(capture, mcpResource, mcpID, exchangeID.String())
	mcpSettled, err := evidence.NewMCPInvocationSettled(mcpID, time.Now(), execution.Succeeded, execution.Occurred, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := capture.AppendExecutionEvidence(mcpSettled); err != nil {
		t.Fatal(err)
	}
	last, err := startModelInvocation(&state, providerCommand(2), capture, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	settleModel(last)
	if ordinal != 4 || len(capture.events) != 8 {
		t.Fatalf("mixed producer ordinals=%d facts=%d, want four full lifecycles", ordinal, len(capture.events))
	}
	for i, kind := range []evidence.ModelPurpose{evidence.ExchangeAttempt, evidence.TargetCharacterization, "", evidence.ExchangeAttempt} {
		started, ok := capture.events[i*2].(evidence.InvocationStartedEvent)
		if !ok || started.ID().ExchangeID() != exchangeID || started.Ordinal() != uint64(i+1) {
			t.Fatalf("invocation %d start = %#v", i, capture.events[i*2])
		}
		settled, ok := capture.events[i*2+1].(evidence.InvocationSettledEvent)
		if !ok || settled.ID() != started.ID() || settled.Outcome() != execution.Succeeded {
			t.Fatalf("invocation %d terminal = %#v", i, capture.events[i*2+1])
		}
		if kind == "" {
			if _, ok := capture.events[i*2].(evidence.MCPInvocationStarted); !ok {
				t.Fatalf("invocation %d is not MCP", i)
			}
		} else if model, ok := capture.events[i*2].(evidence.ModelInvocationStarted); !ok || model.Model().Purpose() != kind {
			t.Fatalf("invocation %d model purpose = %#v", i, capture.events[i*2])
		}
	}
	workspace, err := routing.ParseWorkspaceSlug("work")
	if err != nil {
		t.Fatal(err)
	}
	started, err := evidence.NewExchangeStarted(exchangeID, accessidentity.Local, workspace, time.Now(), clientfamily.Codex, protocolkind.Responses, canonical.NormalizedPathResponses)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := store.NewStore(store.Config{})
	if err := lifecycle.Append(started); err != nil {
		t.Fatal(err)
	}
	for _, event := range capture.events {
		if err := lifecycle.Append(event); err != nil {
			t.Fatalf("mixed invocation rejected by lifecycle store: %v", err)
		}
	}
	closed, err := evidence.NewExchangeSettled(exchangeID, time.Now(), delivery.Succeeded, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Append(closed); err != nil {
		t.Fatal(err)
	}
	if records := lifecycle.Settled(); len(records) != 1 || len(records[0].Invocations) != 4 {
		t.Fatalf("settled mixed lifecycle = %#v", records)
	}
}
