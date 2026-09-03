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
		"usage":{"input_tokens":40,"output_tokens":5,"cache_read_input_tokens":28,"cache_creation_input_tokens":12,"output_tokens_details":{"thinking_tokens":3}}
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
	if !ok || input != 80 {
		t.Fatalf("input tokens = (%d,%v), want (80,true)", input, ok)
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
	if reasoning, ok := out.Usage().ReasoningTokens(); !ok || reasoning != 3 {
		t.Fatalf("reasoning = (%d,%v), want (3,true)", reasoning, ok)
	}
}

func TestDecodeResponseBufferedKeepsInclusiveCompatibilityInput(t *testing.T) {
	raw := []byte(`{
		"id":"msg_1","model":"compat","stop_reason":"end_turn",
		"content":[{"type":"text","text":"ok"}],
		"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":80}}
	}`)
	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex_compat_usage", nil)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if input, ok := response.Usage().InputTokens(); !ok || input != 100 {
		t.Fatalf("inclusive input = (%d,%t), want (100,true)", input, ok)
	}
	if cacheRead, ok := response.Usage().CacheReadTokens(); !ok || cacheRead != 80 {
		t.Fatalf("cache read = (%d,%t), want (80,true)", cacheRead, ok)
	}
}

func TestDecodeResponseBufferedChoosesCompatibilityFamilyAtomically(t *testing.T) {
	raw := []byte(`{
		"id":"msg_1","model":"m","stop_reason":"end_turn",
		"content":[{"type":"text","text":"ok"}],
		"usage":{"prompt_tokens":100,"completion_tokens":5,"cache_read_input_tokens":80}
	}`)
	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex_usage", nil)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if input, ok := response.Usage().InputTokens(); !ok || input != 100 {
		t.Fatalf("mixed compatibility input = (%d,%t), want inclusive (100,true)", input, ok)
	}
	if cacheRead, ok := response.Usage().CacheReadTokens(); ok || cacheRead != 0 {
		t.Fatalf("native cache field leaked into compatibility family = (%d,%t)", cacheRead, ok)
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
	if input, ok := response.Usage().InputTokens(); !ok || input != 29 {
		t.Fatalf("input tokens = (%d,%t), want (29,true)", input, ok)
	}
	if output, ok := response.Usage().OutputTokens(); !ok || output != 15 {
		t.Fatalf("output tokens = (%d,%t), want (15,true)", output, ok)
	}
	if cacheRead, ok := response.Usage().CacheReadTokens(); !ok || cacheRead != 4 {
		t.Fatalf("cache-read tokens = (%d,%t), want (4,true)", cacheRead, ok)
	}
}

func TestDecodeResponseStreamDoesNotMergeConflictingUsageFamilies(t *testing.T) {
	tests := []struct {
		name       string
		startUsage string
		deltaUsage string
		wantInput  int
		wantOutput int
		wantCache  int
		hasCache   bool
	}{
		{
			name:       "native then compatibility",
			startUsage: `{"input_tokens":40,"output_tokens":1,"cache_read_input_tokens":20}`,
			deltaUsage: `{"prompt_tokens":100,"completion_tokens":5}`,
			wantInput:  100,
			wantOutput: 5,
		},
		{
			name:       "compatibility then native",
			startUsage: `{"prompt_tokens":100,"completion_tokens":1}`,
			deltaUsage: `{"input_tokens":40,"output_tokens":5,"cache_read_input_tokens":20}`,
			wantInput:  60,
			wantOutput: 5,
			wantCache:  20,
			hasCache:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[],\"usage\":" + test.startUsage + "}}\n\n" +
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"done\"}}\n\n" +
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":" + test.deltaUsage + "}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
			reader := decodeResponseStream(
				canonical.CanonicalRequest{}, nil,
				carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
				"ex_conflicting_usage", nil,
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
			if input, ok := response.Usage().InputTokens(); !ok || input != test.wantInput {
				t.Fatalf("input = (%d,%t), want (%d,true)", input, ok, test.wantInput)
			}
			if output, ok := response.Usage().OutputTokens(); !ok || output != test.wantOutput {
				t.Fatalf("output = (%d,%t), want (%d,true)", output, ok, test.wantOutput)
			}
			cache, hasCache := response.Usage().CacheReadTokens()
			if cache != test.wantCache || hasCache != test.hasCache {
				t.Fatalf("cache read = (%d,%t), want (%d,%t)", cache, hasCache, test.wantCache, test.hasCache)
			}
		})
	}
}

func TestMessagesUsageFromCanonicalDecomposesCacheInclusiveInput(t *testing.T) {
	input, output, cacheRead, cacheWrite, reasoning := 80, 5, 28, 12, 3
	usage, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{
		InputTokens: &input, OutputTokens: &output, ReasoningTokens: &reasoning,
		CacheReadTokens: &cacheRead, CacheWriteTokens: &cacheWrite,
	})
	dto := messagesUsageFromCanonical(usage, 0, false)
	if dto == nil || dto.InputTokens != 40 || dto.CacheReadInputTokens != 28 || dto.CacheCreationInputTokens != 12 {
		t.Fatalf("decomposed usage = %#v", dto)
	}
	if dto.OutputTokenDetails == nil || dto.OutputTokenDetails.ThinkingTokens != 3 {
		t.Fatalf("thinking detail = %#v", dto.OutputTokenDetails)
	}
}

func TestMessagesUsageFromCanonicalOmitsIncompleteCacheBreakdown(t *testing.T) {
	input, output, cacheRead := 80, 5, 28
	usage, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: &input, OutputTokens: &output, CacheReadTokens: &cacheRead})
	dto := messagesUsageFromCanonical(usage, 0, false)
	if dto == nil || dto.InputTokens != 80 || dto.OutputTokens != 5 || dto.CacheReadInputTokens != 0 || dto.CacheCreationInputTokens != 0 {
		t.Fatalf("partial usage = %#v", dto)
	}
}

func TestMessagesUsageProjectionPreservesUnknownBaseCounters(t *testing.T) {
	value := 7
	tests := []struct {
		name       string
		params     canonical.TokenUsageParams
		wantInput  bool
		wantOutput bool
		wantDetail bool
	}{
		{name: "input only", params: canonical.TokenUsageParams{InputTokens: &value}, wantInput: true},
		{name: "output only", params: canonical.TokenUsageParams{OutputTokens: &value}, wantOutput: true},
		{name: "detail only", params: canonical.TokenUsageParams{ReasoningTokens: &value}, wantDetail: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage, err := canonical.NewTokenUsage(test.params)
			if err != nil {
				t.Fatal(err)
			}
			if projected := messagesUsageFromCanonical(usage, 0, false); projected != nil {
				t.Fatalf("buffered Messages projected unknown base as zero: %#v", projected)
			}
			delta := messagesDeltaUsageFromCanonical(usage, 0, false)
			if (delta.InputTokens != nil) != test.wantInput || (delta.OutputTokens != nil) != test.wantOutput || (delta.OutputTokenDetails != nil) != test.wantDetail {
				t.Fatalf("streamed Messages usage = %#v", delta)
			}
		})
	}
}
