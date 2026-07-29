package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	chatcompletions "github.com/swobuforge/swobu/internal/wire/chatcompletions"
	messages "github.com/swobuforge/swobu/internal/wire/messages"
	responses "github.com/swobuforge/swobu/internal/wire/responses"
)

func TestChatCompletionsCodec_EncodeResponse_MapsUsage(t *testing.T) {
	usage := mustUsage(t, 100, 7, 64, 5)
	output := canonicaltest.ResponseWithUsage(t, "chatcmpl_1",
		"m",
		[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok")},
		canonical.Completed("stop"),
		usage,
	)
	doc, err := (chatcompletions.ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, output)
	if err != nil {
		t.Fatalf("encodeBuffered returned error: %v", err)
	}
	var dto map[string]any
	if err := json.Unmarshal(doc.Document.Raw, &dto); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	assertUsageFieldNumber(t, dto, "usage.prompt_tokens", 100)
	assertUsageFieldNumber(t, dto, "usage.completion_tokens", 7)
	assertUsageFieldNumber(t, dto, "usage.prompt_tokens_details.cached_tokens", 64)
	assertUsageFieldNumber(t, dto, "usage.prompt_tokens_details.cache_write_tokens", 5)
}

func TestChatCompletionsCodec_EncodeResponse_MapsReasoningUsage(t *testing.T) {
	input := 100
	outputTokens := 7
	reasoning := 6
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{
		InputTokens:     &input,
		OutputTokens:    &outputTokens,
		ReasoningTokens: &reasoning,
	})
	if err != nil {
		t.Fatalf("NewTokenUsage returned error: %v", err)
	}
	output := canonicaltest.ResponseWithUsage(t, "chatcmpl_reasoning",
		"m",
		[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok")},
		canonical.Completed("stop"),
		usage,
	)
	doc, err := (chatcompletions.ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, output)
	if err != nil {
		t.Fatalf("encodeBuffered returned error: %v", err)
	}
	var dto map[string]any
	if err := json.Unmarshal(doc.Document.Raw, &dto); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	assertUsageFieldNumber(t, dto, "usage.completion_tokens_details.reasoning_tokens", 6)
}

func TestResponsesCodec_EncodeResponse_MapsUsage(t *testing.T) {
	usage := mustUsage(t, 80, 9, 33, 2)
	output := canonicaltest.ResponseWithUsage(t, "resp_1",
		"m",
		[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok")},
		canonical.Completed("completed"),
		usage,
	)
	doc, err := (responses.ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, output)
	if err != nil {
		t.Fatalf("encodeBuffered returned error: %v", err)
	}
	var dto map[string]any
	if err := json.Unmarshal(doc.Document.Raw, &dto); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	assertUsageFieldNumber(t, dto, "usage.input_tokens", 80)
	assertUsageFieldNumber(t, dto, "usage.output_tokens", 9)
	assertUsageFieldNumber(t, dto, "usage.input_tokens_details.cached_tokens", 33)
	assertUsageFieldNumber(t, dto, "usage.input_tokens_details.cache_write_tokens", 2)
}

func TestResponsesCodec_EncodeResponse_MapsReasoningUsage(t *testing.T) {
	input := 80
	outputTokens := 9
	reasoning := 4
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{
		InputTokens:     &input,
		OutputTokens:    &outputTokens,
		ReasoningTokens: &reasoning,
	})
	if err != nil {
		t.Fatalf("NewTokenUsage returned error: %v", err)
	}
	output := canonicaltest.ResponseWithUsage(t, "resp_reasoning",
		"m",
		[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok")},
		canonical.Completed("completed"),
		usage,
	)
	doc, err := (responses.ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, output)
	if err != nil {
		t.Fatalf("encodeBuffered returned error: %v", err)
	}
	var dto map[string]any
	if err := json.Unmarshal(doc.Document.Raw, &dto); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	assertUsageFieldNumber(t, dto, "usage.output_tokens_details.reasoning_tokens", 4)
}

func TestResponsesCodec_EncodeResponse_UsageIncludesCachedTokensWhenZeroButPresent(t *testing.T) {
	input, output := 12, 3
	cacheRead, cacheWrite := 0, 0
	usage, err := canonical.NewTokenUsageWithOptional(&input, &output, &cacheRead, &cacheWrite)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}
	outputValue := canonicaltest.ResponseWithUsage(t, "resp_compat",
		"m",
		[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok")},
		canonical.Completed("completed"),
		usage,
	)
	doc, err := (responses.ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, outputValue)
	if err != nil {
		t.Fatalf("encodeBuffered returned error: %v", err)
	}
	var dto map[string]any
	if err := json.Unmarshal(doc.Document.Raw, &dto); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	assertUsageFieldNumber(t, dto, "usage.input_tokens_details.cached_tokens", 0)
}

func TestMessagesCodec_EncodeResponse_MapsUsage(t *testing.T) {
	usage := mustUsage(t, 51, 4, 20, 10)
	output := canonicaltest.ResponseWithUsage(t, "msg_1",
		"claude",
		[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok")},
		canonical.Completed("end_turn"),
		usage,
	)
	doc, err := (messages.ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, output)
	if err != nil {
		t.Fatalf("encodeBuffered returned error: %v", err)
	}
	var dto map[string]any
	if err := json.Unmarshal(doc.Document.Raw, &dto); err != nil {
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
