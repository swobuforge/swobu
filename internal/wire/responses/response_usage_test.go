package responses

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponseBuffered_MapsInputOutputAndCacheUsage(t *testing.T) {
	raw := []byte(`{
		"id":"resp_1",
		"model":"m",
		"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],
		"usage":{"input_tokens":91,"output_tokens":6,"output_tokens_details":{"reasoning_tokens":4},"input_tokens_details":{"cached_tokens":64,"cache_write_tokens":3}}
	}`)
	sink := &recordingDecisionSink{}

	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex_usage", sink)
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
	if len(sink.effects) != 6 {
		t.Fatalf("captured effects len=%d want=6", len(sink.effects))
	}
	inputEffect := sink.effects[0]
	if inputEffect.Feature != compat.ResponseUsageInputTokens || inputEffect.Outcome != compat.Exact || inputEffect.Subject != compat.Subject("wire:/usage/input_tokens") {
		t.Fatalf("captured effect[0] = %#v, want usage.input_tokens exact wire:/usage/input_tokens", inputEffect)
	}
	outputEffect := sink.effects[1]
	if outputEffect.Feature != compat.ResponseUsageOutputTokens || outputEffect.Outcome != compat.Exact || outputEffect.Subject != compat.Subject("wire:/usage/output_tokens") {
		t.Fatalf("captured effect[1] = %#v, want usage.output_tokens exact wire:/usage/output_tokens", outputEffect)
	}
	reasoningEffect := sink.effects[2]
	if reasoningEffect.Feature != compat.ResponseUsageReasoningTokens || reasoningEffect.Outcome != compat.Exact || reasoningEffect.Subject != compat.Subject("wire:/usage/output_tokens_details/reasoning_tokens") {
		t.Fatalf("captured effect[2] = %#v, want usage.reasoning_tokens exact wire:/usage/output_tokens_details/reasoning_tokens", reasoningEffect)
	}
	cacheReadEffect := sink.effects[3]
	if cacheReadEffect.Feature != compat.ResponseUsageCacheReadTokens || cacheReadEffect.Outcome != compat.Exact || cacheReadEffect.Subject != compat.Subject("wire:/usage/cache_read_tokens") {
		t.Fatalf("captured effect[3] = %#v, want usage.cache_read_tokens exact wire:/usage/cache_read_tokens", cacheReadEffect)
	}
	cacheWriteEffect := sink.effects[4]
	if cacheWriteEffect.Feature != compat.ResponseUsageCacheWriteTokens || cacheWriteEffect.Outcome != compat.Exact || cacheWriteEffect.Subject != compat.Subject("wire:/usage/cache_write_tokens") {
		t.Fatalf("captured effect[4] = %#v, want usage.cache_write_tokens exact wire:/usage/cache_write_tokens", cacheWriteEffect)
	}
	responseIDEffect := sink.effects[5]
	if responseIDEffect.Feature != compat.ResponseIDResponses || responseIDEffect.Outcome != compat.Exact || responseIDEffect.Subject != compat.Subject("wire:/id") {
		t.Fatalf("captured effect[5] = %#v, want response.id.responses exact wire:/id", responseIDEffect)
	}
}

func TestDecodeResponseStream_EmitsUsageBeforeTerminalDecision(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_1\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"ok\",\"usage\":{\"output_tokens\":2}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"

	sink := &recordingDecisionSink{}
	reader := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_stream_usage", sink)

	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	if _, err := closed.ProjectResponse(); err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	if len(sink.effects) != 3 {
		t.Fatalf("captured effects len=%d want=3", len(sink.effects))
	}
	responseIDEffect := sink.effects[0]
	if responseIDEffect.Feature != compat.ResponseIDResponses || responseIDEffect.Outcome != compat.Exact || responseIDEffect.Subject != compat.Subject("wire:/id") {
		t.Fatalf("captured effect[0] = %#v, want response.id.responses exact wire:/id", responseIDEffect)
	}
	usageEffect := sink.effects[1]
	if usageEffect.Feature != compat.ResponseUsageOutputTokens || usageEffect.Outcome != compat.Exact || usageEffect.Subject != compat.Subject("wire:/usage/output_tokens") {
		t.Fatalf("captured effect[0] = %#v, want usage.output_tokens exact wire:/usage/output_tokens", usageEffect)
	}
	terminalEffect := sink.effects[2]
	if terminalEffect.Feature != compat.DeliveryTerminalEvent || terminalEffect.Outcome != compat.Exact || terminalEffect.Subject != compat.Subject("wire:/event/terminal") {
		t.Fatalf("captured effect[1] = %#v, want delivery.terminal_event exact wire:/event/terminal", terminalEffect)
	}
}

func TestDecodeResponseStream_UsesCompletedOutputFallbackWhenNoDeltas(t *testing.T) {
	t.Parallel()

	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"

	sink := &recordingDecisionSink{}
	reader := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_stream_fallback", sink)

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
