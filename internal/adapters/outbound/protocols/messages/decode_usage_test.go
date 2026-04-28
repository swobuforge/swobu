package messages

import "testing"

func TestDecodeBufferedResult_MapsAnthropicCacheReadWriteUsage(t *testing.T) {
	raw := []byte(`{
		"id":"msg_1",
		"model":"claude-x",
		"stop_reason":"end_turn",
		"content":[{"type":"text","text":"ok"}],
		"usage":{"input_tokens":40,"output_tokens":5,"cache_read_input_tokens":28,"cache_creation_input_tokens":12}
	}`)

	out, err := DecodeBufferedResult(raw)
	if err != nil {
		t.Fatalf("DecodeBufferedResult returned error: %v", err)
	}
	input, ok := out.Usage().InputTokens()
	if !ok || input != 40 {
		t.Fatalf("input tokens = (%d,%v), want (40,true)", input, ok)
	}
	output, ok := out.Usage().OutputTokens()
	if !ok || output != 5 {
		t.Fatalf("output tokens = (%d,%v), want (5,true)", output, ok)
	}
	cacheRead, ok := out.Usage().CacheReadTokens()
	if !ok || cacheRead != 28 {
		t.Fatalf("cache read = (%d,%v), want (28,true)", cacheRead, ok)
	}
	cacheWrite, ok := out.Usage().CacheWriteTokens()
	if !ok || cacheWrite != 12 {
		t.Fatalf("cache write = (%d,%v), want (12,true)", cacheWrite, ok)
	}
}
