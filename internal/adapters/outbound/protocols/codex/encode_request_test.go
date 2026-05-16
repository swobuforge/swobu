package codex

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
}

func TestRealize_RejectsBufferedDelivery(t *testing.T) {
	t.Parallel()

	_, err := EncodeRequest(
		canonical.NewGenerationRequest(canonical.GenerationRequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "hello"),
			},
		}),
		false,
	)
	if err == nil {
		t.Fatal("expected unsupported delivery error")
	}
}
