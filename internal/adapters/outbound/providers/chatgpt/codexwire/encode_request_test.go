package codexwire

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestRealize_NormalizesCodexPayload(t *testing.T) {
	t.Parallel()

	wireReq, err := EncodeRequest(
		canonical.NewGenerationRequest(canonical.GenerationRequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "hello"),
			},
		}),
		true,
	)
	if err != nil {
		t.Fatalf("realize: %v", err)
	}
	raw, err := io.ReadAll(wireReq.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	instructions, _ := payload["instructions"].(string)
	if instructions == "" {
		t.Fatal("expected instructions")
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
	content, ok := first["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty content, got %#v", first["content"])
	}
	part, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("expected object content[0], got %#v", content[0])
	}
	if got := part["type"]; got != "output_text" {
		t.Fatalf("expected content type output_text, got %#v", got)
	}
}

func TestRealize_AcceptsBufferedClientPreferenceViaStreamNativeEncoding(t *testing.T) {
	t.Parallel()

	wireReq, err := EncodeRequest(
		canonical.NewGenerationRequest(canonical.GenerationRequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "hello"),
			},
		}),
		false,
	)
	if err != nil {
		t.Fatalf("realize: %v", err)
	}
	raw, err := io.ReadAll(wireReq.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if stream, ok := payload["stream"].(bool); !ok || !stream {
		t.Fatalf("expected stream=true for codex-native request, got %#v", payload["stream"])
	}
}
