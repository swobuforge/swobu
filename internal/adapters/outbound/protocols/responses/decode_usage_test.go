package responses

import "testing"

func TestDecodeResponseBuffered_MapsInputOutputAndCacheUsage(t *testing.T) {
	raw := []byte(`{
		"id":"resp_1",
		"model":"m",
		"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],
		"usage":{"input_tokens":91,"output_tokens":6,"input_tokens_details":{"cached_tokens":64,"cache_write_tokens":3}}
	}`)

	out, err := DecodeResponseBuffered(raw)
	if err != nil {
		t.Fatalf("DecodeResponseBuffered returned error: %v", err)
	}
	input, ok := out.Usage().InputTokens()
	if !ok || input != 91 {
		t.Fatalf("input tokens = (%d,%v), want (91,true)", input, ok)
	}
	output, ok := out.Usage().OutputTokens()
	if !ok || output != 6 {
		t.Fatalf("output tokens = (%d,%v), want (6,true)", output, ok)
	}
	cacheRead, ok := out.Usage().CacheReadTokens()
	if !ok || cacheRead != 64 {
		t.Fatalf("cache read = (%d,%v), want (64,true)", cacheRead, ok)
	}
	cacheWrite, ok := out.Usage().CacheWriteTokens()
	if !ok || cacheWrite != 3 {
		t.Fatalf("cache write = (%d,%v), want (3,true)", cacheWrite, ok)
	}
}
