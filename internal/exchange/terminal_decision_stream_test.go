package exchange

import (
	"context"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
)

type effectRecordingSink struct {
	effects []compat.Decision
}

func (s *effectRecordingSink) Commit(_ context.Context, _ string, effects []compat.Decision) error {
	s.effects = append(s.effects, effects...)
	return nil
}

type testDecisionSource struct{ decisions []compat.Decision }

type pullingClientCodec struct {
	testClientCodec
	terminal provider.DecisionSource
}

func (c pullingClientCodec) EncodeResponseStream(ctx context.Context, _ canonical.CanonicalRequest, events canonical.ResponseStream, _ delivery.Delivery) (wire.ClientByteStreamResult, error) {
	completion, complete, fail := wire.NewResponseCompletion()
	body := wire.NewEncodedResponseBody(ctx, events, func(event canonical.Event) ([][]byte, error) {
		if event.Kind == canonical.EventEnvelopeEnd {
			complete(testHistoryResponse([]byte("ok")))
		}
		return [][]byte{[]byte("event\n")}, nil
	}, completion, fail)
	return wire.ClientByteStreamResult{Stream: carrier.ByteStream{MediaType: "text/event-stream", Body: body}, TerminalDecisions: c.terminal, Completion: completion}, nil
}

func (s testDecisionSource) Decisions() []compat.Decision {
	return append([]compat.Decision(nil), s.decisions...)
}

func TestStreamingCommitsExplicitTerminalCompatibilityDecisions(t *testing.T) {
	sink := &effectRecordingSink{}
	reader := canonical.NewSliceEventReader([]canonical.Event{
		{
			Kind:  canonical.EventEnvelopeStart,
			EnvID: "r1",
			Payload: canonical.EnvelopeStartPayload{
				Kind: canonical.EnvResponse,
			},
		},
		{
			Kind:  canonical.EventEnvelopeEnd,
			EnvID: "r1",
			Payload: canonical.EnvelopeEndPayload{
				Kind:   canonical.EnvResponse,
				Status: canonical.EnvelopeStatusCompleted,
			},
		},
	})
	decisions := testDecisionSource{decisions: []compat.Decision{{
		Feature: compat.ResponseUsageReasoningTokens,
		Outcome: compat.Drop,
		Subject: "test:terminal-stream",
	}}}
	clientDecisions := testDecisionSource{decisions: []compat.Decision{{
		Feature: compat.ResponseItemsKind,
		Outcome: compat.Drop,
		Subject: "web_search:completed_pair",
	}}}

	in := ExchangeInput{
		ExchangeID:       "ex_reader_effects",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    "alpha",
		Target:           provider.NewTargetSnapshot("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
	}

	call := providerCall{
		backend:     provider.Backend{Target: in.Target},
		request:     provider.Request{Canonical: in.Request, Delivery: in.ProviderDelivery},
		clientCodec: pullingClientCodec{terminal: clientDecisions}, clientDelivery: in.ClientDelivery,
		exchangeID: in.ExchangeID, workspaceSlug: in.WorkspaceSlug, fullRequest: in.Request,
	}
	out, err := encodeClientOutput(context.Background(), call, newTerminalCompatibilityStream(reader, decisions, sink, in.ExchangeID), true, sink, nil)
	if err != nil {
		t.Fatalf("encodeClientOutput returned error: %v", err)
	}
	if ClientTransportForTest(out).Body == nil {
		t.Fatal("streaming transport body was nil")
	}
	if _, err := io.ReadAll(ClientTransportForTest(out).Body); err != nil {
		t.Fatalf("consume streaming body: %v", err)
	}

	foundProvider := false
	foundClient := false
	for _, decision := range sink.effects {
		if decision.Feature == compat.ResponseUsageReasoningTokens {
			foundProvider = true
			if decision.Outcome != compat.Drop {
				t.Fatalf("decision outcome = %q, want drop", decision.Outcome)
			}
		}
		if decision.Feature == compat.ResponseItemsKind && decision.Subject == "web_search:completed_pair" {
			foundClient = true
		}
	}
	if !foundProvider || !foundClient {
		t.Fatalf("terminal compatibility decisions were not committed: %#v", sink.effects)
	}
}
