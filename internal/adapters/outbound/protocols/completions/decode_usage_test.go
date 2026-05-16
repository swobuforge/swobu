package completions

import "testing"

func TestDecodeResponseBuffered_MapsUsageAndCacheFields(t *testing.T) {
	raw := []byte(`{
		"id":"cmpl_1",
		"model":"m",
		"choices":[{"text":"ok","finish_reason":"stop"}],
		"usage":{"prompt_tokens":44,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":20,"cache_write_tokens":2}}
	}`)

	out, err := DecodeResponseBuffered(raw)
	if err != nil {
		t.Fatalf("DecodeResponseBuffered returned error: %v", err)
	}
	input, ok := out.Usage().InputTokens()
	if !ok || input != 44 {
		t.Fatalf("input tokens = (%d,%v), want (44,true)", input, ok)
	}
	output, ok := out.Usage().OutputTokens()
	if !ok || output != 3 {
		t.Fatalf("output tokens = (%d,%v), want (3,true)", output, ok)
	}
	cacheRead, ok := out.Usage().CacheReadTokens()
	if !ok || cacheRead != 20 {
		t.Fatalf("cache read = (%d,%v), want (20,true)", cacheRead, ok)
	}
	cacheWrite, ok := out.Usage().CacheWriteTokens()
	if !ok || cacheWrite != 2 {
		t.Fatalf("cache write = (%d,%v), want (2,true)", cacheWrite, ok)
	}
}
