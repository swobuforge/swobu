package httpapi

import (
	"encoding/json"
	"testing"

	chatcompletions "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/chat_completions"
	messages "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/messages"
	responses "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/responses"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestChatCompletionsCodec_EncodeBuffered_MapsUsage(t *testing.T) {
	usage := mustUsage(t, 100, 7, 64, 5)
	output := canonical.NewConversationOutputWithUsage(
		"chatcmpl_1",
		"m",
		[]canonical.CanonicalItem{canonical.NewTextOutputItem("text_0", "ok")},
		"stop",
		usage,
	)
	raw, err := (chatcompletions.ChatCompletionsFamilyCodec{}).EncodeBuffered(output)
	if err != nil {
		t.Fatalf("encodeBuffered returned error: %v", err)
	}
	var dto map[string]any
	if err := json.Unmarshal(raw, &dto); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	assertUsageFieldNumber(t, dto, "usage.prompt_tokens", 100)
	assertUsageFieldNumber(t, dto, "usage.completion_tokens", 7)
	assertUsageFieldNumber(t, dto, "usage.prompt_tokens_details.cached_tokens", 64)
	assertUsageFieldNumber(t, dto, "usage.prompt_tokens_details.cache_write_tokens", 5)
}

func TestResponsesCodec_EncodeBuffered_MapsUsage(t *testing.T) {
	usage := mustUsage(t, 80, 9, 33, 2)
	output := canonical.NewConversationOutputWithUsage(
		"resp_1",
		"m",
		[]canonical.CanonicalItem{canonical.NewTextOutputItem("text_0", "ok")},
		"completed",
		usage,
	)
	raw, err := (responses.ResponsesFamilyCodec{}).EncodeBuffered(output)
	if err != nil {
		t.Fatalf("encodeBuffered returned error: %v", err)
	}
	var dto map[string]any
	if err := json.Unmarshal(raw, &dto); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	assertUsageFieldNumber(t, dto, "usage.input_tokens", 80)
	assertUsageFieldNumber(t, dto, "usage.output_tokens", 9)
	assertUsageFieldNumber(t, dto, "usage.input_tokens_details.cached_tokens", 33)
	assertUsageFieldNumber(t, dto, "usage.input_tokens_details.cache_write_tokens", 2)
}

func TestResponsesCodec_EncodeBuffered_UsageIncludesCachedTokensWhenZeroButPresent(t *testing.T) {
	input, output := 12, 3
	cacheRead, cacheWrite := 0, 0
	usage, err := canonical.NewTokenUsageWithOptional(&input, &output, &cacheRead, &cacheWrite)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}
	outputValue := canonical.NewConversationOutputWithUsage(
		"resp_compat",
		"m",
		[]canonical.CanonicalItem{canonical.NewTextOutputItem("text_0", "ok")},
		"completed",
		usage,
	)
	raw, err := (responses.ResponsesFamilyCodec{}).EncodeBuffered(outputValue)
	if err != nil {
		t.Fatalf("encodeBuffered returned error: %v", err)
	}
	var dto map[string]any
	if err := json.Unmarshal(raw, &dto); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	assertUsageFieldNumber(t, dto, "usage.input_tokens_details.cached_tokens", 0)
}

func TestMessagesCodec_EncodeBuffered_MapsUsage(t *testing.T) {
	usage := mustUsage(t, 51, 4, 20, 10)
	output := canonical.NewConversationOutputWithUsage(
		"msg_1",
		"claude",
		[]canonical.CanonicalItem{canonical.NewTextOutputItem("text_0", "ok")},
		"end_turn",
		usage,
	)
	raw, err := (messages.MessagesFamilyCodec{}).EncodeBuffered(output)
	if err != nil {
		t.Fatalf("encodeBuffered returned error: %v", err)
	}
	var dto map[string]any
	if err := json.Unmarshal(raw, &dto); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	assertUsageFieldNumber(t, dto, "usage.input_tokens", 51)
	assertUsageFieldNumber(t, dto, "usage.output_tokens", 4)
	assertUsageFieldNumber(t, dto, "usage.cache_read_input_tokens", 20)
	assertUsageFieldNumber(t, dto, "usage.cache_creation_input_tokens", 10)
}

func mustUsage(t *testing.T, input, output, cacheRead, cacheWrite int) canonical.TokenUsage {
	t.Helper()
	usage, err := canonical.NewTokenUsageWithOptional(&input, &output, &cacheRead, &cacheWrite)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}
	return usage
}

func assertUsageFieldNumber(t *testing.T, dto map[string]any, path string, want float64) {
	t.Helper()
	value, ok := lookupNumber(dto, path)
	if !ok {
		t.Fatalf("%s missing in response", path)
	}
	if value != want {
		t.Fatalf("%s = %v, want %v", path, value, want)
	}
}

func lookupNumber(root map[string]any, path string) (float64, bool) {
	current := any(root)
	segments := splitPath(path)
	for _, segment := range segments {
		object, ok := current.(map[string]any)
		if !ok {
			return 0, false
		}
		next, ok := object[segment]
		if !ok {
			return 0, false
		}
		current = next
	}
	number, ok := current.(float64)
	return number, ok
}

func splitPath(path string) []string {
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] != '.' {
			continue
		}
		parts = append(parts, path[start:i])
		start = i + 1
	}
	parts = append(parts, path[start:])
	return parts
}
