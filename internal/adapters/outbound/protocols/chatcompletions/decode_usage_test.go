package chatcompletions

import "testing"

func TestDecodeBufferedResult_MapsUsageAndCacheFields(t *testing.T) {
	raw := []byte(`{
		"id":"chatcmpl_1",
		"model":"m",
		"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":120,"completion_tokens":8,"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":4}}
	}`)

	out, err := DecodeBufferedResult(raw)
	if err != nil {
		t.Fatalf("DecodeBufferedResult returned error: %v", err)
	}
	input, ok := out.Usage().InputTokens()
	if !ok || input != 120 {
		t.Fatalf("input tokens = (%d,%v), want (120,true)", input, ok)
	}
	output, ok := out.Usage().OutputTokens()
	if !ok || output != 8 {
		t.Fatalf("output tokens = (%d,%v), want (8,true)", output, ok)
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
