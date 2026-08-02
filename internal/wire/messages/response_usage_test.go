package messages

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponseBuffered_MapsAnthropicCacheReadWriteUsage(t *testing.T) {
	raw := []byte(`{
		"id":"msg_1",
		"model":"claude-x",
		"stop_reason":"end_turn",
		"content":[{"type":"text","text":"ok"}],
		"usage":{"input_tokens":40,"output_tokens":5,"cache_read_input_tokens":28,"cache_creation_input_tokens":12}
	}`)

	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex_usage", nil)
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

func TestDecodeResponseStreamMergesCumulativeUsageAcrossStartAndDelta(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":25,\"output_tokens\":1,\"cache_read_input_tokens\":4}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"done\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":15}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	reader := decodeResponseStream(
		canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex_usage", nil,
	)
	closed, err := canonical.ReadClosedEnvelope(
		context.Background(),
		canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}),
		canonical.EnvResponse,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if input, ok := response.Usage().InputTokens(); !ok || input != 25 {
		t.Fatalf("input tokens = (%d,%t), want (25,true)", input, ok)
	}
	if output, ok := response.Usage().OutputTokens(); !ok || output != 15 {
		t.Fatalf("output tokens = (%d,%t), want (15,true)", output, ok)
	}
	if cacheRead, ok := response.Usage().CacheReadTokens(); !ok || cacheRead != 4 {
		t.Fatalf("cache-read tokens = (%d,%t), want (4,true)", cacheRead, ok)
	}
}
