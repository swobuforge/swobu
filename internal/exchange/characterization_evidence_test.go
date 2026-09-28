package exchange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/execution"
	evidence "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCharacterizationNotDispatchedDoesNotAllocateInvocation(t *testing.T) {
	exchangeID, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	capture := &characterizationEvidenceCapture{}
	allocations := 0
	transport := &characterizationTransport{
		delegate: provider.TransportFunc(func(context.Context, carrier.Document) (provider.Ingress, error) {
			return nil, provider.AttemptNotDispatched(errors.New("local transport preparation failed"))
		}),
		nextInvocation: func() execution.InvocationID {
			allocations++
			id, _ := execution.NewInvocationID(exchangeID, uint64(allocations))
			return id
		},
		sink:   capture,
		target: provider.TargetSnapshot{TargetID: "target", TargetVersion: 1, ProviderSpec: "openai", Model: "model", ProtocolKind: protocolkind.Responses},
	}
	_, err = transport.Send(context.Background(), carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{}))
	if err == nil || allocations != 0 || len(capture.events) != 0 {
		t.Fatalf("not-dispatched characterization: err=%v allocations=%d events=%d", err, allocations, len(capture.events))
	}
}

type characterizationEvidenceCapture struct{ events []evidence.Event }

type failingCharacterizationEvidenceSink struct{}

func (failingCharacterizationEvidenceSink) AppendExecutionEvidence(evidence.Event) error {
	return context.Canceled
}

func (c *characterizationEvidenceCapture) AppendExecutionEvidence(event evidence.Event) error {
	c.events = append(c.events, event)
	return nil
}

func TestCharacterizationEvidenceFailureDoesNotAbortPhysicalDispatch(t *testing.T) {
	executed := false
	target := provider.TargetSnapshot{TargetID: "target", TargetVersion: 1, ProviderSpec: "openai", Model: "model", ProtocolKind: protocolkind.Responses}
	transport := &characterizationTransport{
		delegate: provider.TransportFunc(func(context.Context, carrier.Document) (provider.Ingress, error) {
			executed = true
			return provider.DocumentIngress{Document: carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{"ok":true}`), carrier.Meta{})}, nil
		}),
		nextInvocation: func() execution.InvocationID { return execution.InvocationID{} },
		sink:           failingCharacterizationEvidenceSink{}, target: target,
	}
	if _, err := transport.Send(context.Background(), carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{})); err != nil {
		t.Fatal(err)
	}
	if !executed {
		t.Fatal("evidence recorder failure prevented physical characterization dispatch")
	}
}

var _ observation.ExecutionEvidenceSink = (*characterizationEvidenceCapture)(nil)

func TestCharacterizationTransportDefersSettlementUntilResponseConsumption(t *testing.T) {
	exchangeID, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	_, err = routing.ParseWorkspaceSlug("workspace")
	capture := &characterizationEvidenceCapture{}
	route, err := routing.ParseRouteName("primary")
	if err != nil {
		t.Fatal(err)
	}
	target := provider.TargetSnapshot{TargetID: "target", TargetVersion: 1, ProviderSpec: "openai", Model: "model", ProtocolKind: protocolkind.Responses}
	ordinal := uint64(0)
	transport := &characterizationTransport{
		delegate: provider.TransportFunc(func(context.Context, carrier.Document) (provider.Ingress, error) {
			return provider.DocumentIngress{Document: carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{"ok":true}`), carrier.Meta{})}, nil
		}),
		nextInvocation: func() execution.InvocationID {
			ordinal++
			id, _ := execution.NewInvocationID(exchangeID, ordinal)
			return id
		}, sink: capture, target: target, route: route,
	}
	_, err = transport.Send(context.Background(), carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(capture.events) != 1 {
		t.Fatalf("events after ingress = %d, want one start only", len(capture.events))
	}
	if _, ok := capture.events[0].(evidence.ModelInvocationStarted); !ok {
		t.Fatalf("first event = %T, want ModelInvocationStarted", capture.events[0])
	}
	transport.FixtureSettled(nil)
	if len(capture.events) != 2 {
		t.Fatalf("events after consumption = %d, want start and settlement", len(capture.events))
	}
	settled, ok := capture.events[1].(evidence.ModelInvocationSettled)
	if !ok || string(settled.Outcome()) != "succeeded" || string(settled.Occurrence()) != "occurred" {
		t.Fatalf("settlement = %#v (%T)", capture.events[1], capture.events[1])
	}
}

func TestCharacterizationFixtureSettlementsStayIndependent(t *testing.T) {
	exchangeID, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	capture := &characterizationEvidenceCapture{}
	route, err := routing.ParseRouteName("primary")
	if err != nil {
		t.Fatal(err)
	}
	ordinal := uint64(0)
	transport := &characterizationTransport{
		delegate: provider.TransportFunc(func(context.Context, carrier.Document) (provider.Ingress, error) {
			return provider.DocumentIngress{Document: carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{})}, nil
		}),
		nextInvocation: func() execution.InvocationID {
			ordinal++
			id, _ := execution.NewInvocationID(exchangeID, ordinal)
			return id
		}, sink: capture, route: route,
		target: provider.TargetSnapshot{TargetID: "target", TargetVersion: 1, ProviderSpec: "openai", Model: "model", ProtocolKind: protocolkind.Responses},
	}
	for range 2 {
		if _, err := transport.Send(context.Background(), carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{})); err != nil {
			t.Fatal(err)
		}
	}
	// A response-invalid fixture cannot rewrite a later successful one.
	transport.FixtureSettled(errors.New("fixture response invalid"))
	transport.FixtureSettled(nil)
	if ordinal != 2 || len(capture.events) != 4 {
		t.Fatalf("ordinal=%d events=%d, want two starts and two settlements", ordinal, len(capture.events))
	}
	first := capture.events[2].(evidence.ModelInvocationSettled)
	second := capture.events[3].(evidence.ModelInvocationSettled)
	if first.Outcome() != execution.Failed || first.Failure() != evidence.FailureResponseInvalid || second.Outcome() != execution.Succeeded || first.ID().Ordinal() != 1 || second.ID().Ordinal() != 2 {
		t.Fatalf("independent fixture settlements = (%#v, %#v)", first, second)
	}
}

func TestCharacterizationSendCancellationSettlesOnlyDispatchedFixture(t *testing.T) {
	exchangeID, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	route, err := routing.ParseRouteName("primary")
	if err != nil {
		t.Fatal(err)
	}
	ordinal := uint64(0)
	capture := &characterizationEvidenceCapture{}
	transport := &characterizationTransport{
		delegate: provider.TransportFunc(func(context.Context, carrier.Document) (provider.Ingress, error) {
			return nil, provider.AttemptMayHaveExecuted(provider.Cancelled(context.Canceled))
		}),
		nextInvocation: func() execution.InvocationID {
			ordinal++
			id, _ := execution.NewInvocationID(exchangeID, ordinal)
			return id
		},
		sink: capture, route: route,
		target: provider.TargetSnapshot{TargetID: "target", TargetVersion: 1, ProviderSpec: "openai", Model: "model", ProtocolKind: protocolkind.Responses},
	}
	_, err = transport.Send(context.Background(), carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{}`), carrier.Meta{}))
	if !errors.Is(err, context.Canceled) || ordinal != 1 || len(capture.events) != 2 {
		t.Fatalf("send cancellation: err=%v ordinal=%d events=%d", err, ordinal, len(capture.events))
	}
	settled := capture.events[1].(evidence.ModelInvocationSettled)
	if settled.Outcome() != execution.Cancelled || settled.Failure() != "" || settled.Occurrence() != execution.MayHaveOccurred {
		t.Fatalf("send cancellation settled as %#v", settled)
	}
}

func TestInvalidModelResourceDoesNotConsumeInvocationOrdinal(t *testing.T) {
	exchangeID, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	ordinal := uint64(0)
	state := exchangeState{
		input: exchangeInput{canonicalExchangeID: exchangeID}, invocationOrdinal: &ordinal,
		providerCallAttempts: []providerCallAttempt{{}},
	}
	_, err = startModelInvocation(&state, callProviderCommand{attemptID: 1, backend: provider.Backend{Target: provider.TargetSnapshot{}}}, nil, time.Now())
	if err == nil || ordinal != 0 || state.providerCallAttempts[0].executionInvocationID.Valid() {
		t.Fatalf("invalid resource consumed identity: err=%v ordinal=%d attempt=%#v", err, ordinal, state.providerCallAttempts[0])
	}
}

func TestCharacterizationMixedFixtureEvidenceUsesEachTerminal(t *testing.T) {
	for _, matching := range []bool{true, false} {
		name := "mismatched"
		if matching {
			name = "matching"
		}
		t.Run(name, func(t *testing.T) {
			id, err := execution.NewExchangeID()
			if err != nil {
				t.Fatal(err)
			}
			route, err := routing.ParseRouteName("primary")
			if err != nil {
				t.Fatal(err)
			}
			capture := &characterizationEvidenceCapture{}
			ordinal, calls := uint64(0), 0
			target := provider.TargetSnapshot{TargetID: "target", TargetVersion: 1, ProviderSpec: "openai", Model: "model", ProtocolKind: protocolkind.Responses}
			transport := &characterizationTransport{
				sink: capture, target: target, route: route,
				delegate: provider.TransportFunc(func(context.Context, carrier.Document) (provider.Ingress, error) {
					calls++
					if calls == 2 {
						return provider.DocumentIngress{Document: carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{"id":"response","model":"model","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}]}`), carrier.Meta{})}, nil
					}
					param := "first"
					if calls == 3 && !matching {
						param = "different"
					}
					return nil, provider.AttemptMayHaveExecuted(provider.Rejected(canonical.NewStructuredBackendError("target", protocolkind.Responses, 400, canonical.BackendErrorDetail{Type: "invalid_request_error", Code: "unsupported_value", Param: param}, "")))
				}),
				nextInvocation: func() execution.InvocationID {
					ordinal++
					invocation, _ := execution.NewInvocationID(id, ordinal)
					return invocation
				},
			}
			backend := provider.Backend{Target: target, Codec: protocolcodec.Codec{Protocol: protocolkind.Responses}, Transport: transport}
			resolution := backend.CharacterizeTargetFact(context.Background(), provider.AcceptsReasoningDisabled)
			if resolution.Conclusive != matching || resolution.Value || calls != 3 || ordinal != 3 || len(capture.events) != 6 {
				t.Fatalf("resolution=%#v calls=%d ordinal=%d events=%d", resolution, calls, ordinal, len(capture.events))
			}
			for i, want := range []execution.Outcome{execution.Failed, execution.Succeeded, execution.Failed} {
				started := capture.events[i*2].(evidence.ModelInvocationStarted)
				settled := capture.events[i*2+1].(evidence.ModelInvocationSettled)
				if started.ID().Ordinal() != uint64(i+1) || settled.ID() != started.ID() || settled.Outcome() != want {
					t.Fatalf("fixture %d start=%#v settlement=%#v, want %q", i, started, settled, want)
				}
				if i == 1 && (settled.Occurrence() != execution.Occurred || settled.Failure() != "") {
					t.Fatalf("successful control rewritten as %#v", settled)
				}
				if i != 1 && settled.Failure() != evidence.FailureRejected {
					t.Fatalf("rejected fixture %d failure = %q", i, settled.Failure())
				}
			}
		})
	}
}
