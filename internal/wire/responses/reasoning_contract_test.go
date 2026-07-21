package responses

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

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
	document, err := EncodeCarrierWithDecisions(EncodeInput{Request: request}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"summary":"auto"`) || !strings.Contains(string(document.RawBytes()), `"effort":"high"`) || !strings.Contains(string(document.RawBytes()), `"reasoning.encrypted_content"`) {
		t.Fatalf("encoded = %s", document.RawBytes())
	}
}

func TestResponsesPreservesClientEncryptedContinuation(t *testing.T) {
	raw := []byte(`{"model":"gpt","input":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"brief"}],"encrypted_content":"secret"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.ResponsesInput.JSONObjects()
	if len(items) != 1 || !strings.Contains(string(items[0]), `"encrypted_content":"secret"`) {
		t.Fatalf("native input was not preserved: %s", items)
	}
}

func TestResponsesReasoningSummaryIsAtomic(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt")})
	raw := []byte(`{"id":"resp","model":"gpt","status":"completed","output":[{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"brief"}]}]}`)
	stream, err := decodeResponseBuffered(context.Background(), request, raw, "ex", nil)
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
	buffered, err := decodeResponseBuffered(context.Background(), request, []byte(`{"id":"resp","model":"gpt","status":"completed","output":[{"type":"reasoning","id":"rs_1","status":"completed","summary":[]}]}`), "ex", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertNoReasoningItem(t, buffered)

	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"in_progress\",\"summary\":[]}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"completed\",\"summary\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"completed\",\"output\":[]}}\n\n"
	assertNoReasoningItem(t, decodeResponseStream(request, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil))
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

func TestResponsesNativeContinuationUsesProviderHandle(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt"), PreviousResponse: &canonical.ResponseRef{
			SwobuID: "resp", Responses: &canonical.ResponsesNativeRef{ProviderResponseID: "provider_resp", TargetID: "target", TargetVersion: 1},
		},
	})
	document, err := EncodeCarrierWithDecisions(EncodeInput{Request: request}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"previous_response_id":"provider_resp"`) {
		t.Fatalf("encoded = %s", document.RawBytes())
	}
}
