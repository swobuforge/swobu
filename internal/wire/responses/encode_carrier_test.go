package responses

import (
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func EncodeCarrier(request canonical.CanonicalRequest, d delivery.Delivery) (carrier.Document, error) {
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		return carrier.Document{}, err
	}
	return EncodeCarrierWithChanges(EncodeInput{Request: request, ToolNames: names}, d, nil, "", EncodeOptions{})
}

func TestApplyAttemptDecoration_RejectsReservedSemanticKeys(t *testing.T) {
	reserved := []string{
		"model", "input", "instructions", "tools", "tool_choice",
		"stream", "temperature", "top_p", "max_output_tokens", "stop",
		"response_format", "text", "include", "store", "previous_response_id",
	}
	for _, key := range reserved {
		t.Run(key, func(t *testing.T) {
			payload := map[string]any{"model": "test"}
			err := ApplyAttemptDecoration(payload, map[string]any{key: "forbidden"})
			if err == nil {
				t.Fatalf("expected error when decorating reserved key %q", key)
			}
		})
	}

	payload := map[string]any{"model": "test"}
	if err := ApplyAttemptDecoration(payload, map[string]any{"custom_header": "value"}); err != nil {
		t.Fatalf("unexpected error decorating non-semantic keys: %v", err)
	}
	if payload["custom_header"] != "value" {
		t.Fatalf("decoration not applied: %#v", payload)
	}
}
