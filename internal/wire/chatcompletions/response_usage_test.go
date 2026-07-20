package chatcompletions

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type recordingDecisionSink struct {
	effects []compat.Decision
}

func (s *recordingDecisionSink) Commit(_ context.Context, _ string, effects []compat.Decision) error {
	s.effects = append(s.effects, effects...)
	return nil
}

func TestDecodeResponseBuffered_MapsUsageAndCacheFields(t *testing.T) {
	raw := []byte(`{
		"id":"chatcmpl_1",
		"model":"m",
		"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":120,"completion_tokens":8,"completion_tokens_details":{"reasoning_tokens":5},"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":4}}
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
	if len(sink.effects) != 5 {
		t.Fatalf("captured effects len=%d want=5", len(sink.effects))
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
	if reasoningEffect.Feature != compat.ResponseUsageReasoningTokens || reasoningEffect.Outcome != compat.Exact || reasoningEffect.Subject != compat.Subject("wire:/usage/completion_tokens_details/reasoning_tokens") {
		t.Fatalf("captured effect[2] = %#v, want usage.reasoning_tokens exact wire:/usage/completion_tokens_details/reasoning_tokens", reasoningEffect)
	}
	cacheReadEffect := sink.effects[3]
	if cacheReadEffect.Feature != compat.ResponseUsageCacheReadTokens || cacheReadEffect.Outcome != compat.Exact || cacheReadEffect.Subject != compat.Subject("wire:/usage/cache_read_tokens") {
		t.Fatalf("captured effect[3] = %#v, want usage.cache_read_tokens exact wire:/usage/cache_read_tokens", cacheReadEffect)
	}
	cacheWriteEffect := sink.effects[4]
	if cacheWriteEffect.Feature != compat.ResponseUsageCacheWriteTokens || cacheWriteEffect.Outcome != compat.Exact || cacheWriteEffect.Subject != compat.Subject("wire:/usage/cache_write_tokens") {
		t.Fatalf("captured effect[4] = %#v, want usage.cache_write_tokens exact wire:/usage/cache_write_tokens", cacheWriteEffect)
	}
}

func TestDecodeResponseStream_EmitsUsageBeforeTerminalDecision(t *testing.T) {
	raw := "data: {\"id\":\"chatcmpl_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"\"}],\"usage\":{\"completion_tokens\":8}}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"model\":\"m\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"

	sink := &recordingDecisionSink{}
	reader := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_stream_usage", sink)

	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	if _, err := closed.ProjectResponse(); err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	if len(sink.effects) != 2 {
		t.Fatalf("captured effects len=%d want=2", len(sink.effects))
	}
	usageEffect := sink.effects[0]
	if usageEffect.Feature != compat.ResponseUsageOutputTokens || usageEffect.Outcome != compat.Exact || usageEffect.Subject != compat.Subject("wire:/usage/output_tokens") {
		t.Fatalf("captured effect[0] = %#v, want usage.output_tokens exact wire:/usage/output_tokens", usageEffect)
	}
	terminalEffect := sink.effects[1]
	if terminalEffect.Feature != compat.DeliveryTerminalEvent || terminalEffect.Outcome != compat.Exact || terminalEffect.Subject != compat.Subject("wire:/event/terminal") {
		t.Fatalf("captured effect[1] = %#v, want delivery.terminal_event exact wire:/event/terminal", terminalEffect)
	}
}
