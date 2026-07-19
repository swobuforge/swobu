package chatgpt

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestBackendCodecNormalizesCodexPayload(t *testing.T) {
	t.Parallel()

	doc, _, err := newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
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

func TestBackendCodecRejectsBufferedProviderDelivery(t *testing.T) {
	_, _, err := newBackendCodec("chatgpt").Encode(provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
		}),
		Delivery: delivery.BufferedDelivery(),
	})
	if err == nil {
		t.Fatal("buffered provider delivery must be rejected")
	}
}

func TestBackendInternalStoreFalseDoesNotSuppressNativeResponseCapture(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"provider_resp_789\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"provider_resp_789\",\"status\":\"completed\",\"output\":[]}}\n\n"
	decoded, err := newBackendCodec("chatgpt").Decode(context.Background(), "ex_store_false", provider.StreamIngress{Stream: carrier.ByteStream{
		MediaType: "text/event-stream",
		Body:      io.NopCloser(strings.NewReader(raw)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), decoded.Stream, canonical.EnvResponse)
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
}
