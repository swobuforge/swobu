package chatgpt

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestBackendCodecPreservesRawJSONIntegers(t *testing.T) {
	document, _, err := newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: canonicaltest.LargeIntegerRequest(t, "gpt-5.4-mini"),
		Delivery:  delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(document.RawBytes(), []byte("9007199254740993")); got != 3 {
		t.Fatalf("large integer occurrences = %d, want 3: %s", got, document.RawBytes())
	}
}

func TestBackendCodecNormalizesCodexPayload(t *testing.T) {
	t.Parallel()

	doc, _, err := newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gpt-5.4-mini"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		}),
		Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := payload["instructions"]; ok {
		t.Fatalf("instructions overlay must be absent, got %#v", payload["instructions"])
	}
	if store, ok := payload["store"].(bool); !ok || store {
		t.Fatalf("store=%#v, want false", payload["store"])
	}
	if stream, ok := payload["stream"].(bool); !ok || !stream {
		t.Fatalf("stream=%#v, want true", payload["stream"])
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) == 0 {
		t.Fatalf("input=%#v, want non-empty list", payload["input"])
	}
	first, ok := input[0].(map[string]any)
	if !ok || first["type"] != "message" || first["role"] != "user" || first["content"] != "hello" {
		t.Fatalf("input[0]=%#v, want canonical user message", input[0])
	}
}

func TestBackendCodecLowersWebSearchToChatGPTResponsesMarker(t *testing.T) {
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt-5.4-mini"), Tools: canonical.Specify(set)})
	document, _, err := newBackendCodec("chatgpt").Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(document.RawBytes(), []byte(`"tools":[{"type":"web_search"}]`)) {
		t.Fatalf("web-search marker = %s", document.RawBytes())
	}
}

func TestBackendCodecRejectsBufferedProviderDelivery(t *testing.T) {
	_, _, err := newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gpt-5.4-mini"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		}),
		Delivery: delivery.BufferedDelivery(),
	})
	if err == nil {
		t.Fatal("buffered provider delivery must be rejected")
	}
}

func TestBackendInternalStoreFalseDoesNotSuppressNativeResponseCapture(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"provider_resp_789\",\"model\":\"gpt-5.4-mini\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"provider_resp_789\",\"status\":\"completed\",\"output\":[]}}\n\n"
	decoded, err := newBackendCodec("chatgpt").Decode(context.Background(), provider.Request{ExchangeID: "ex_store_false"}, provider.StreamIngress{Stream: carrier.ByteStream{
		MediaType: "text/event-stream",
		Body:      io.NopCloser(strings.NewReader(raw)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	target := provider.NewTargetSnapshot("chatgpt-target", "chatgpt", "https://chatgpt.test", "", "responses", "", "responses")
	target.Model = "gpt-5.4-mini"
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(decoded.Stream, canonical.ResponseBinding{SwobuID: "resp_test", TargetID: target.TargetID, TargetVersion: target.TargetVersion}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	output, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	responsesRef := output.Response().Responses
	if responsesRef == nil || responsesRef.ProviderResponseID != "provider_resp_789" {
		t.Fatalf("native response refinement = %#v", responsesRef)
	}
	next := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify("gpt-5.4-mini"),
		Items:            []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "again")},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_test", Responses: responsesRef},
	})
	document, _, err := newBackendCodec("chatgpt").Encode(provider.Request{Canonical: next, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["previous_response_id"] != "provider_resp_789" {
		t.Fatalf("native continuation payload = %s", document.RawBytes())
	}
}
