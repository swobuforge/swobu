package codexwire

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestRealize_NormalizesCodexPayload(t *testing.T) {
	t.Parallel()

	wireReq, err := EncodeProviderRequestDocument(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "hello"),
			},
		}),
		delivery.StreamingDelivery(delivery.FramingSSE),
		"",
	)
	if err != nil {
		t.Fatalf("realize: %v", err)
	}
	raw := wireReq.Value.Raw
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := payload["instructions"]; ok {
		t.Fatalf("expected no instructions overlay, got %#v", payload["instructions"])
	}
	if store, ok := payload["store"].(bool); !ok || store {
		t.Fatalf("expected store=false, got %#v", payload["store"])
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) == 0 {
		t.Fatalf("expected list input, got %#v", payload["input"])
	}
	first, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("expected object input[0], got %#v", input[0])
	}
	if got := first["type"]; got != "message" {
		t.Fatalf("expected content type message, got %#v", got)
	}
	if got := first["role"]; got != "user" {
		t.Fatalf("expected role user, got %#v", got)
	}
	content, ok := first["content"].(string)
	if !ok || content == "" {
		t.Fatalf("expected non-empty string content, got %#v", first["content"])
	}
	if content != "hello" {
		t.Fatalf("expected content hello, got %#v", content)
	}
}

func TestRealize_AcceptsBufferedClientPreferenceViaStreamNativeEncoding(t *testing.T) {
	t.Parallel()

	wireReq, err := EncodeProviderRequestDocument(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "hello"),
			},
		}),
		delivery.BufferedDelivery(),
		"",
	)
	if err != nil {
		t.Fatalf("realize: %v", err)
	}
	raw := wireReq.Value.Raw
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if stream, ok := payload["stream"].(bool); !ok || !stream {
		t.Fatalf("expected stream=true for codex-native request, got %#v", payload["stream"])
	}
}
