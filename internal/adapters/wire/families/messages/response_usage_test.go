package messages

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
)

type recordingEffectSink struct {
	effects []effect.Effect
}

func (s *recordingEffectSink) Commit(_ context.Context, _ string, effects []effect.Effect) error {
	s.effects = append(s.effects, effects...)
	return nil
}

func TestDecodeResponseBuffered_MapsAnthropicCacheReadWriteUsage(t *testing.T) {
	raw := []byte(`{
		"id":"msg_1",
		"model":"claude-x",
		"stop_reason":"end_turn",
		"content":[{"type":"text","text":"ok"}],
		"usage":{"input_tokens":40,"output_tokens":5,"cache_read_input_tokens":28,"cache_creation_input_tokens":12}
	}`)

	sink := &recordingEffectSink{}

	reader, err := decodeResponseBuffered(context.Background(), raw, "ex_usage", sink)
	if err != nil {
		t.Fatalf("DecodeResponseBuffered returned error: %v", err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), reader, canonical.EnvResponse)
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
	if len(sink.effects) != 4 {
		t.Fatalf("captured effects len=%d want=4", len(sink.effects))
	}
	inputEffect, ok := sink.effects[0].(effect.Compatibility)
	if !ok {
		t.Fatalf("captured effect[0] type = %T, want effect.Compatibility", sink.effects[0])
	}
	if inputEffect.Feature != compat.UsageInputTokens || inputEffect.Outcome != compat.Exact || inputEffect.Subject != compat.Subject("wire:/usage/input_tokens") {
		t.Fatalf("captured effect[0] = %#v, want usage.input_tokens exact wire:/usage/input_tokens", inputEffect)
	}
	outputEffect, ok := sink.effects[1].(effect.Compatibility)
	if !ok {
		t.Fatalf("captured effect[1] type = %T, want effect.Compatibility", sink.effects[1])
	}
	if outputEffect.Feature != compat.UsageOutputTokens || outputEffect.Outcome != compat.Exact || outputEffect.Subject != compat.Subject("wire:/usage/output_tokens") {
		t.Fatalf("captured effect[1] = %#v, want usage.output_tokens exact wire:/usage/output_tokens", outputEffect)
	}
	cacheReadEffect, ok := sink.effects[2].(effect.Compatibility)
	if !ok {
		t.Fatalf("captured effect[2] type = %T, want effect.Compatibility", sink.effects[2])
	}
	if cacheReadEffect.Feature != compat.UsageCacheReadTokens || cacheReadEffect.Outcome != compat.Exact || cacheReadEffect.Subject != compat.Subject("wire:/usage/cache_read_tokens") {
		t.Fatalf("captured effect[2] = %#v, want usage.cache_read_tokens exact wire:/usage/cache_read_tokens", cacheReadEffect)
	}
	cacheWriteEffect, ok := sink.effects[3].(effect.Compatibility)
	if !ok {
		t.Fatalf("captured effect[3] type = %T, want effect.Compatibility", sink.effects[3])
	}
	if cacheWriteEffect.Feature != compat.UsageCacheWriteTokens || cacheWriteEffect.Outcome != compat.Exact || cacheWriteEffect.Subject != compat.Subject("wire:/usage/cache_write_tokens") {
		t.Fatalf("captured effect[3] = %#v, want usage.cache_write_tokens exact wire:/usage/cache_write_tokens", cacheWriteEffect)
	}
}

func TestDecodeResponseStream_EmitsUsageBeforeTerminalDecision(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-x\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"},\"usage\":{\"output_tokens\":5}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	sink := &recordingEffectSink{}
	reader := decodeResponseStream(carrier.WireStream{Frames: carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader(raw)))}, "ex_stream_usage", sink)

	closed, err := canonical.ReadClosedEnvelope(context.Background(), reader, canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	if _, err := closed.ProjectResponse(); err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	if len(sink.effects) != 2 {
		t.Fatalf("captured effects len=%d want=2", len(sink.effects))
	}
	usageEffect, ok := sink.effects[0].(effect.Compatibility)
	if !ok {
		t.Fatalf("captured effect[0] type = %T, want effect.Compatibility", sink.effects[0])
	}
	if usageEffect.Feature != compat.UsageOutputTokens || usageEffect.Outcome != compat.Exact || usageEffect.Subject != compat.Subject("wire:/usage/output_tokens") {
		t.Fatalf("captured effect[0] = %#v, want usage.output_tokens exact wire:/usage/output_tokens", usageEffect)
	}
	terminalEffect, ok := sink.effects[1].(effect.Compatibility)
	if !ok {
		t.Fatalf("captured effect[1] type = %T, want effect.Compatibility", sink.effects[1])
	}
	if terminalEffect.Feature != compat.DeliveryTerminalEvent || terminalEffect.Outcome != compat.Exact || terminalEffect.Subject != compat.Subject("wire:/event/terminal") {
		t.Fatalf("captured effect[1] = %#v, want delivery.terminal_event exact wire:/event/terminal", terminalEffect)
	}
}
