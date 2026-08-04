package responses

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestResponsesReasoningContextDecodesAndEncodesExactly(t *testing.T) {
	for _, value := range []canonical.ResponsesReasoningContext{
		canonical.ResponsesReasoningContextAuto,
		canonical.ResponsesReasoningContextAllTurns,
		canonical.ResponsesReasoningContextCurrentTurn,
	} {
		t.Run(string(value), func(t *testing.T) {
			raw := []byte(`{"model":"gpt","input":"hi","reasoning":{"context":"` + string(value) + `"}}`)
			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
			if err != nil {
				t.Fatal(err)
			}
			got, present := decoded.Request.Request.Reasoning().ResponsesContextField().Get()
			if !present || got != value {
				t.Fatalf("decoded context = (%q,%t), want (%q,true)", got, present, value)
			}
			document, err := EncodeCarrierWithChanges(EncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(document.RawBytes()), `"context":"`+string(value)+`"`) {
				t.Fatalf("encoded = %s", document.RawBytes())
			}
			result, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(
				wire.ProviderEncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)},
				delivery.BufferedDelivery(),
				"exchange",
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Changes) != 0 {
				t.Fatalf("exact reasoning context changes = %#v", result.Changes)
			}
		})
	}
}

func TestResponsesReasoningContextRejectsMalformedValuesAsBadRequest(t *testing.T) {
	for _, rawContext := range []string{`"future"`, `""`, `17`, `{}`} {
		raw := []byte(`{"model":"gpt","input":"hi","reasoning":{"context":` + rawContext + `}}`)
		_, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
		var canonicalErr canonical.Error
		if !errors.As(err, &canonicalErr) || canonicalErr.Code != canonical.ErrorCodeBadRequest {
			t.Fatalf("context %s error = %#v, want typed bad request", rawContext, err)
		}
	}
}

func TestResponsesReasoningContextSurvivesImplicitRebase(t *testing.T) {
	raw := []byte(`{
		"model":"gpt",
		"reasoning":{"context":"current_turn"},
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]},
			{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"answer"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Request.RebasedRequest == nil {
		t.Fatal("expected implicit Responses rebase")
	}
	for name, request := range map[string]canonical.CanonicalRequest{
		"full":    decoded.Request.Request,
		"rebased": decoded.Request.RebasedRequest.Request,
	} {
		contextValue, present := request.Reasoning().ResponsesContextField().Get()
		if !present || contextValue != canonical.ResponsesReasoningContextCurrentTurn {
			t.Fatalf("%s context = (%q,%t)", name, contextValue, present)
		}
	}
}

func TestResponsesReasoningRequestNormalizesToMinimalControls(t *testing.T) {
	raw := []byte(`{"model":"gpt","input":"hi","reasoning":{"effort":"high","summary":"detailed"}}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	request := decoded.Request.Request
	compute, ok := request.Reasoning().ComputeField().Get()
	if !ok || compute.Kind() != canonical.ReasoningAutomatic {
		t.Fatalf("compute = %#v", compute)
	}
	document, err := EncodeCarrierWithChanges(EncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"summary":"auto"`) || !strings.Contains(string(document.RawBytes()), `"effort":"high"`) || !strings.Contains(string(document.RawBytes()), `"reasoning.encrypted_content"`) {
		t.Fatalf("encoded = %s", document.RawBytes())
	}
}

func TestResponsesFutureReasoningQualityHintsApproximateToOmission(t *testing.T) {
	raw := []byte(`{"model":"gpt","input":"hi","reasoning":{"effort":"future_effort","summary":"future_summary"}}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Request.Request.Reasoning().ComputeField().IsSpecified() ||
		decoded.Request.Request.Reasoning().DisclosureField().IsSpecified() {
		t.Fatalf("future reasoning hints survived canonically: %#v", decoded.Request.Request.Reasoning())
	}
	if len(decoded.Changes) != 1 {
		t.Fatalf("changes = %#v, want one deduplicated approximation", decoded.Changes)
	}
	for _, decision := range decoded.Changes {
		if decision.Capability != canonical.RequestReasoning || decision.Kind != compat.Approximation {
			t.Fatalf("decision = %#v, want request.reasoning Approx", decision)
		}
	}
}

func TestResponsesPreservesClientEncryptedContinuation(t *testing.T) {
	raw := []byte(`{"model":"gpt","input":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"brief"}],"encrypted_content":"secret"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	reasoning, ok := items[0].Reasoning()
	if len(items) != 1 || !ok {
		t.Fatalf("canonical reasoning was not preserved: %#v", items)
	}
	replay, ok := reasoning.Opaque().Responses()
	if !ok || replay.EncryptedContent != "secret" {
		t.Fatalf("encrypted reasoning replay = %#v", replay)
	}
}

func TestResponsesReasoningSummaryIsAtomic(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt")})
	raw := []byte(`{"id":"resp","model":"gpt","status":"completed","output":[{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"brief"}]}]}`)
	stream, err := decodeResponseBuffered(context.Background(), request, testAttemptToolNames(request), raw, "ex", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			break
		}
		if event.Kind == canonical.EventItemStart || event.Kind == canonical.EventContentStart || event.Kind == canonical.EventTextDelta {
			if payload, ok := event.Payload.(canonical.ItemEvent); ok && payload.Position.Item == 0 {
				t.Fatalf("reasoning emitted progressive event %q", event.Kind)
			}
		}
		if event.Kind == canonical.EventItemCompleted {
			item := event.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item
			found = found || item.Kind() == canonical.ItemKindReasoning
		}
	}
	if !found {
		t.Fatal("reasoning summary completion was not emitted")
	}
}

func TestResponsesEmptyReasoningArtifactsAreIgnored(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt")})
	buffered, err := decodeResponseBuffered(context.Background(), request, testAttemptToolNames(request), []byte(`{"id":"resp","model":"gpt","status":"completed","output":[{"type":"reasoning","id":"rs_1","status":"completed","summary":[]}]}`), "ex", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	assertNoReasoningItem(t, buffered)

	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"in_progress\",\"summary\":[]}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"completed\",\"summary\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"completed\",\"output\":[]}}\n\n"
	assertNoReasoningItem(t, decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true))
}

func assertNoReasoningItem(t *testing.T, stream canonical.ResponseStream) {
	t.Helper()
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			return
		}
		if event.Kind == canonical.EventItemCompleted {
			item := event.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item
			if item.Kind() == canonical.ItemKindReasoning {
				t.Fatal("empty reasoning artifact produced a canonical item")
			}
		}
	}
}

func TestResponsesContinuationUsesProviderHandle(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt"),
	})
	document, err := EncodeCarrierWithChanges(EncodeInput{Request: request, ResponsesPrevious: &provider.ResponsesPrevious{ProviderResponseID: "provider_resp", OmitStart: 0, OmitEnd: 0}, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"previous_response_id":"provider_resp"`) {
		t.Fatalf("encoded = %s", document.RawBytes())
	}
}
