package responses

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponseBuffered_MapsInputOutputAndCacheUsage(t *testing.T) {
	raw := []byte(`{
		"id":"resp_1",
		"model":"m",
		"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],
		"usage":{"input_tokens":91,"output_tokens":6,"output_tokens_details":{"reasoning_tokens":4},"input_tokens_details":{"cached_tokens":64,"cache_write_tokens":3}}
	}`)
	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex_usage", nil, true)
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
	if !ok || input != 91 {
		t.Fatalf("input tokens = (%d,%v), want (91,true)", input, ok)
	}
	output, ok := out.Usage().OutputTokens()
	if !ok || output != 6 {
		t.Fatalf("output tokens = (%d,%v), want (6,true)", output, ok)
	}
	reasoning, ok := out.Usage().ReasoningTokens()
	if !ok || reasoning != 4 {
		t.Fatalf("reasoning tokens = (%d,%v), want (4,true)", reasoning, ok)
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

func TestDecodeResponseStream_UsesCompletedOutputFallbackWhenNoDeltas(t *testing.T) {
	t.Parallel()

	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"

	reader := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_stream_fallback", nil, true)

	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	out, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	items := out.Items()
	if len(items) != 1 {
		t.Fatalf("output items len=%d want=1", len(items))
	}
	if items[0].Kind() != canonical.ItemKindMessage {
		t.Fatalf("output item kind=%s want=message", items[0].Kind())
	}
	message, _ := items[0].Message()
	text, _ := message.Content()[0].Text()
	if text.Text() != "ok" {
		t.Fatalf("output text=%q want ok", text.Text())
	}
}

func TestDecodeResponseStreamMergesSparseUsageSnapshotsByField(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[],\"usage\":{\"input_tokens\":20,\"input_tokens_details\":{\"cached_tokens\":10}}}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"output_tokens\":7,\"output_tokens_details\":{\"reasoning_tokens\":3}}}}\n\n"
	reader := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_sparse_usage", nil, true)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if input, ok := response.Usage().InputTokens(); !ok || input != 20 {
		t.Fatalf("input = (%d,%t), want retained (20,true)", input, ok)
	}
	if output, ok := response.Usage().OutputTokens(); !ok || output != 7 {
		t.Fatalf("output = (%d,%t), want latest (7,true)", output, ok)
	}
	if cacheRead, ok := response.Usage().CacheReadTokens(); !ok || cacheRead != 10 {
		t.Fatalf("cache read = (%d,%t), want retained (10,true)", cacheRead, ok)
	}
	if reasoning, ok := response.Usage().ReasoningTokens(); !ok || reasoning != 3 {
		t.Fatalf("reasoning = (%d,%t), want latest (3,true)", reasoning, ok)
	}
}
