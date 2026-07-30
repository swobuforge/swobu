package chatcompletions

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponseBuffered_MapsUsageAndCacheFields(t *testing.T) {
	raw := []byte(`{
		"id":"chatcmpl_1",
		"model":"m",
		"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":120,"completion_tokens":8,"completion_tokens_details":{"reasoning_tokens":5},"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":4}}
	}`)
	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex_usage", nil)
	if err != nil {
		t.Fatalf("DecodeResponseBuffered returned error: %v", err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	out, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	input, ok := out.Usage().InputTokens()
	if !ok || input != 120 {
		t.Fatalf("input tokens = (%d,%v), want (120,true)", input, ok)
	}
	output, ok := out.Usage().OutputTokens()
	if !ok || output != 8 {
		t.Fatalf("output tokens = (%d,%v), want (8,true)", output, ok)
	}
	reasoning, ok := out.Usage().ReasoningTokens()
	if !ok || reasoning != 5 {
		t.Fatalf("reasoning tokens = (%d,%v), want (5,true)", reasoning, ok)
	}
	cacheRead, ok := out.Usage().CacheReadTokens()
	if !ok || cacheRead != 80 {
		t.Fatalf("cache read = (%d,%v), want (80,true)", cacheRead, ok)
	}
	cacheWrite, ok := out.Usage().CacheWriteTokens()
	if !ok || cacheWrite != 4 {
		t.Fatalf("cache write = (%d,%v), want (4,true)", cacheWrite, ok)
	}
}
