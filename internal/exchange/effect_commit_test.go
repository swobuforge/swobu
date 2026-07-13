package exchange_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	responses "github.com/swobuforge/swobu/internal/adapters/wire/families/responses"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	. "github.com/swobuforge/swobu/internal/exchange"
	stage "github.com/swobuforge/swobu/internal/exchange/stage"
)

type recordingEffectSink struct {
	effects []effect.Effect
}

func (s *recordingEffectSink) Commit(_ context.Context, _ string, effects []effect.Effect) error {
	s.effects = append(s.effects, effects...)
	return nil
}

type invariantDocPatch struct {
	id      string
	mutated bool
	effects []effect.Effect
	nextRaw []byte
}

func (t invariantDocPatch) ID() string { return t.id }
func (t invariantDocPatch) Stage() stage.Stage {
	return stage.StageRequestDocumentOut
}
func (t invariantDocPatch) Capabilities() stage.StageCapabilities {
	return stage.StageCapabilities{}
}
func (t invariantDocPatch) Match(stage.Context, carrier.WireDocument) bool { return true }
func (t invariantDocPatch) Apply(_ stage.Context, in carrier.WireDocument) (stage.Result[carrier.WireDocument], error) {
	out := in
	out.Raw = append([]byte(nil), t.nextRaw...)
	return stage.Result[carrier.WireDocument]{
		Value:   out,
		Mutated: t.mutated,
		Effects: append([]effect.Effect(nil), t.effects...),
	}, nil
}

func assertCompatibilityEffect(t *testing.T, got effect.Effect, feature compat.Feature, outcome compat.Outcome, subject compat.Subject) {
	t.Helper()
	compatEffect, ok := got.(effect.CompatibilityEffect)
	if !ok {
		t.Fatalf("effect type = %T, want effect.CompatibilityEffect", got)
	}
	if compatEffect.Feature != feature || compatEffect.Outcome != outcome || compatEffect.Subject != subject {
		t.Fatalf("compatibility effect = %#v, want %s/%s/%s", compatEffect, feature, outcome, subject)
	}
}

type compatibilityEmittingStreamWrapper struct{}

func (compatibilityEmittingStreamWrapper) ID() string { return "test.compatibility" }

func (compatibilityEmittingStreamWrapper) Stage() stage.Stage {
	return stage.StageSemanticEvents
}

func (compatibilityEmittingStreamWrapper) Capabilities() stage.StageCapabilities {
	return stage.StageCapabilities{}
}

func (compatibilityEmittingStreamWrapper) Match(stage.Context, canonical.EventReader) bool {
	return true
}

func (compatibilityEmittingStreamWrapper) Wrap(_ stage.Context, reader canonical.EventReader) (stage.Result[canonical.EventReader], error) {
	return stage.Result[canonical.EventReader]{
		Value: reader,
		Effects: []effect.Effect{
			effect.CompatibilityEffect{
				Feature: compat.ToolCallID,
				Outcome: compat.Approx,
				Subject: compat.Subject("wire:/response/0/tool_calls/0/id"),
			},
			effect.TurnStateEffect{
				Op:    effect.TurnStateReplay,
				Key:   "turn.request.raw",
				Value: []byte("cached-raw"),
			},
		},
	}, nil
}

func TestRunnerRun_PassesThroughCompatibilityAndTurnStateEffects(t *testing.T) {
	sink := &recordingEffectSink{}
	runner := withRuntime(bufferedProviderIngressResolver([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`)))
	runner.StageMechanics = stage.NewStageMechanics(nil, []stage.EventStreamWrapper{compatibilityEmittingStreamWrapper{}})
	runner.EffectSink = sink

	_, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_obs",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		Target:           NewRoutableTarget("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.effects) != 2 {
		t.Fatalf("captured effects len=%d want=2", len(sink.effects))
	}

	compatEffect, ok := sink.effects[0].(effect.CompatibilityEffect)
	if !ok {
		t.Fatalf("effect[0] type = %T, want effect.CompatibilityEffect", sink.effects[0])
	}
	if compatEffect.Feature != compat.ToolCallID || compatEffect.Outcome != compat.Approx || compatEffect.Subject != compat.Subject("wire:/response/0/tool_calls/0/id") {
		t.Fatalf("compatibility effect = %#v, want tool_call_id/approx/wire:/response/0/tool_calls/0/id", compatEffect)
	}

	turnStateEffect, ok := sink.effects[1].(effect.TurnStateEffect)
	if !ok {
		t.Fatalf("effect[1] type = %T, want effect.TurnStateEffect", sink.effects[1])
	}
	if turnStateEffect.Op != effect.TurnStateReplay || turnStateEffect.Key != "turn.request.raw" || !bytes.Equal(turnStateEffect.Value, []byte("cached-raw")) {
		t.Fatalf("turn-state effect = %#v, want replay/turn.request.raw/cached-raw", turnStateEffect)
	}
}

func TestRunnerRun_CommitsCompatibilityEffectBeforeRejectError(t *testing.T) {
	sink := &recordingEffectSink{}
	runner := withRuntime(bufferedProviderIngressResolver([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`)))
	runner.StageMechanics = stage.NewStageMechanics([]stage.DocumentPatch{
		invariantDocPatch{
			id:      "rejecting",
			mutated: false,
			effects: []effect.Effect{
				effect.CompatibilityEffect{
					Feature: compat.RequestStructuredOutput,
					Outcome: compat.Reject,
					Subject: compat.Subject("/input"),
				},
			},
			nextRaw: []byte(`{"model":"m"}`),
		},
	}, nil)
	runner.EffectSink = sink

	_, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_reject",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		Target:           NewRoutableTarget("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
	})
	if err == nil {
		t.Fatal("Run returned nil error, want reject")
	}
	var unsupported UnsupportedProjectionError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Run error = %v, want UnsupportedProjectionError", err)
	}
	if len(sink.effects) != 1 {
		t.Fatalf("captured effects len=%d want=1", len(sink.effects))
	}
	gotReject, ok := sink.effects[0].(effect.CompatibilityEffect)
	if !ok {
		t.Fatalf("effect type = %T, want effect.CompatibilityEffect", sink.effects[0])
	}
	if gotReject.Outcome != compat.Reject || gotReject.Subject != compat.Subject("/input") {
		t.Fatalf("captured reject effect = %#v, want reject /input", gotReject)
	}
}

func TestRunnerRun_RecordsDeliveryStreamingDecisionExact(t *testing.T) {
	sink := &recordingEffectSink{}
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := withRuntime(streamingProviderIngressResolver(io.NopCloser(strings.NewReader(providerSSE))))
	runner.EffectSink = sink

	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_stream_exact",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		Target:           NewRoutableTarget("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !out.Progressive {
		t.Fatal("expected progressive streaming")
	}
	if len(sink.effects) != 3 {
		t.Fatalf("captured effects len=%d want=3", len(sink.effects))
	}
	assertCompatibilityEffect(t, sink.effects[0], compat.DeliveryStreaming, compat.Exact, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[1], compat.DeliveryIncremental, compat.Exact, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[2], compat.DeliveryServerSentEvents, compat.Exact, compat.Subject("route:provider/openai/protocol/responses"))
}

func TestRunnerRun_RecordsDeliveryStreamingDecisionApproxWhenBuffered(t *testing.T) {
	sink := &recordingEffectSink{}
	runner := withRuntime(bufferedProviderIngressResolver([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`)))
	runner.EffectSink = sink

	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_stream_approx",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		Target:           NewRoutableTarget("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContractForDeliveries(delivery.StreamingDelivery(delivery.FramingSSE), delivery.BufferedDelivery()),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if out.Progressive {
		t.Fatal("expected non-progressive streaming when provider is buffered")
	}
	if len(sink.effects) != 3 {
		t.Fatalf("captured effects len=%d want=3", len(sink.effects))
	}
	assertCompatibilityEffect(t, sink.effects[0], compat.DeliveryStreaming, compat.Approx, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[1], compat.DeliveryIncremental, compat.Approx, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[2], compat.DeliveryServerSentEvents, compat.Exact, compat.Subject("route:provider/openai/protocol/responses"))
}

func TestRunnerRun_RecordsDeliveryWebSocketConversionApproxWhenProviderIsSSE(t *testing.T) {
	sink := &recordingEffectSink{}
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := withRuntime(streamingProviderIngressResolver(io.NopCloser(strings.NewReader(providerSSE))))
	runner.EffectSink = sink

	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_stream_websocket",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingWebSocket),
		Request:          testCanonicalRequest("m"),
		Target:           NewRoutableTarget("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContractForDeliveries(delivery.StreamingDelivery(delivery.FramingWebSocket), delivery.StreamingDelivery(delivery.FramingSSE)),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !out.Progressive {
		t.Fatal("expected progressive streaming")
	}
	if len(sink.effects) != 4 {
		t.Fatalf("captured effects len=%d want=4", len(sink.effects))
	}
	assertCompatibilityEffect(t, sink.effects[0], compat.DeliveryStreaming, compat.Exact, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[1], compat.DeliveryIncremental, compat.Exact, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[2], compat.DeliveryServerSentEvents, compat.Approx, compat.Subject("route:provider/openai/protocol/responses"))
	assertCompatibilityEffect(t, sink.effects[3], compat.DeliveryWebSocket, compat.Approx, compat.Subject("route:provider/openai/protocol/responses"))
}

func TestRunnerRun_RecordsDeliveryTerminalEventDropWhenProviderStreamLacksUsage(t *testing.T) {
	sink := &recordingEffectSink{}
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := withRuntime(streamingProviderIngressResolver(io.NopCloser(strings.NewReader(providerSSE))))
	runner.EffectSink = sink

	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_terminal_drop",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		Target:           NewRoutableTarget("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses_stream"),
		Contract:         NewExecutionContractForDeliveries(delivery.BufferedDelivery(), delivery.StreamingDelivery(delivery.FramingSSE)),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if out.Transport.Body == nil {
		t.Fatal("buffered client response must set transport body")
	}
	if len(sink.effects) != 1 {
		t.Fatalf("captured effects len=%d want=1", len(sink.effects))
	}
	assertCompatibilityEffect(t, sink.effects[0], compat.DeliveryTerminalEvent, compat.Drop, compat.Subject("wire:/event/terminal"))
}

func TestRunnerRun_RecordsWireNativePayloadAndTurnStateReplay(t *testing.T) {
	sink := &recordingEffectSink{}
	runner := withRuntime(bufferedProviderIngressResolver([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`)))
	runner.EffectSink = sink

	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		Turn:  canonical.NewTurnRef("resp_prev"),
	})
	codec := responses.ProviderRequestDocumentEncoder{}
	expectedWireResult, err := codec.EncodeProviderRequestDocument(request, delivery.BufferedDelivery(), "")
	if err != nil {
		t.Fatalf("encode provider request document: %v", err)
	}
	expectedWire := expectedWireResult.Value

	_, err = runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_native_payload",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          request,
		Target:           NewRoutableTarget("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.effects) != 2 {
		t.Fatalf("captured effects len=%d want=2", len(sink.effects))
	}

	turnStateEffect, ok := sink.effects[0].(effect.TurnStateEffect)
	if !ok {
		t.Fatalf("effect[0] type = %T, want effect.TurnStateEffect", sink.effects[0])
	}
	if turnStateEffect.Op != effect.TurnStateReplay || turnStateEffect.Key != "turn.request.raw" {
		t.Fatalf("turn-state effect = %#v, want replay/turn.request.raw", turnStateEffect)
	}
	if !bytes.Equal(turnStateEffect.Value, expectedWire.RawBytes()) {
		t.Fatalf("turn-state value = %q, want provider request raw payload", turnStateEffect.Value)
	}

	compatEffect, ok := sink.effects[1].(effect.CompatibilityEffect)
	if !ok {
		t.Fatalf("effect[1] type = %T, want effect.CompatibilityEffect", sink.effects[1])
	}
	if compatEffect.Feature != compat.WireNativePayload || compatEffect.Outcome != compat.Exact {
		t.Fatalf("compatibility effect = %#v, want wire.native_payload exact", compatEffect)
	}
	if compatEffect.Subject != compat.Subject("state:turn.request.raw") {
		t.Fatalf("compatibility subject = %q, want state:turn.request.raw", compatEffect.Subject)
	}
}

func TestRunnerRun_RecordsErrorShapeDropOnBackendError(t *testing.T) {
	sink := &recordingEffectSink{}
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		return nil, canonical.NewBackendError(req.Target.BackendRef, http.StatusBadGateway, `{"error":{"message":"provider failed"}}`, "")
	})
	runner.EffectSink = sink

	_, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_error_shape",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		Target:           NewRoutableTarget("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
	})
	if err == nil {
		t.Fatal("Run returned nil error, want backend error")
	}
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("Run error = %v, want BackendError", err)
	}
	if len(sink.effects) != 1 {
		t.Fatalf("captured effects len=%d want=1", len(sink.effects))
	}
	compatEffect, ok := sink.effects[0].(effect.CompatibilityEffect)
	if !ok {
		t.Fatalf("effect type = %T, want effect.CompatibilityEffect", sink.effects[0])
	}
	if compatEffect.Feature != compat.ErrorShape || compatEffect.Outcome != compat.Drop {
		t.Fatalf("compatibility effect = %#v, want error.shape drop", compatEffect)
	}
	if compatEffect.Subject != compat.Subject("route:provider/openai/protocol/responses") {
		t.Fatalf("compatibility subject = %q, want route subject", compatEffect.Subject)
	}
}
