package chatcompletions

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponseBuffered_MapsUsageAndCacheFields(t *testing.T) {
	raw := []byte(`{
		"id":"chatcmpl_1",
		"model":"m",
		"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":120,"completion_tokens":8,"total_tokens":140,"completion_tokens_details":{"reasoning_tokens":5},"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":4}}
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
	if total, ok := out.Usage().TotalKnownTokens(); !ok || total != 128 {
		t.Fatalf("canonical total = (%d,%v), want derived (128,true)", total, ok)
	}
}

func TestDecodeResponseStreamKeepsLatestCumulativeUsageSnapshot(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"m","choices":[{"index":0,"delta":{"role":"assistant"}}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`,
		``,
		`data: {"id":"chatcmpl_1","model":"m","choices":[{"index":0,"delta":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
		``,
		`data: {"id":"chatcmpl_1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	reader := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_usage", nil)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if output, ok := response.Usage().OutputTokens(); !ok || output != 3 {
		t.Fatalf("output = (%d,%t), want latest snapshot (3,true)", output, ok)
	}
	if total, ok := response.Usage().TotalKnownTokens(); !ok || total != 13 {
		t.Fatalf("canonical total = (%d,%t), want latest snapshot (13,true)", total, ok)
	}
}

func TestDecodeResponseStreamCapturesUsageOnTerminalChoice(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"m","choices":[{"index":0,"delta":{"content":"ok"}}],"usage":null}`,
		``,
		`data: {"id":"chatcmpl_1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":99}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	response := decodeClosedChatUsageResponse(t, raw)
	if total, ok := response.Usage().TotalKnownTokens(); !ok || total != 13 {
		t.Fatalf("canonical total = (%d,%t), want derived terminal-choice usage (13,true)", total, ok)
	}
}

func TestDecodeResponseStreamCapturesOpenAIUsageOnlyFinalChunk(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"m","choices":[{"index":0,"delta":{"role":"assistant"}}],"usage":null}`,
		``,
		`data: {"id":"chatcmpl_1","model":"m","choices":[{"index":0,"delta":{"content":"ok"}}],"usage":null}`,
		``,
		`data: {"id":"chatcmpl_1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":null}`,
		``,
		`data: {"id":"chatcmpl_1","model":"m","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	response := decodeClosedChatUsageResponse(t, raw)
	if total, ok := response.Usage().TotalKnownTokens(); !ok || total != 13 {
		t.Fatalf("canonical total = (%d,%t), want (13,true)", total, ok)
	}
}

func TestDecodeResponseStreamFinishWithoutDoneFailsAtEOF(t *testing.T) {
	raw := "data: {\"id\":\"chatcmpl_1\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":null}\n\n"
	reader := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_usage", nil)
	bound := canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closed.ProjectResponse(); err == nil {
		t.Fatal("stream without [DONE] completed successfully")
	}
}

func TestDecodeResponseStreamSilentUsageIgnoreCompletesUnknownAtDone(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"m","choices":[{"index":0,"delta":{"content":"ok"}}],"usage":null}`,
		``,
		`data: {"id":"chatcmpl_1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":null}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	response := decodeClosedChatUsageResponse(t, raw)
	if !response.Usage().IsZero() {
		t.Fatalf("silently omitted usage became fabricated accounting: %#v", response.Usage())
	}
}

func TestDecodeResponseStreamMergesSparseUsageSnapshotsByField(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"m","choices":[{"index":0,"delta":{"content":"ok"}}],"usage":{"prompt_tokens":20,"prompt_tokens_details":{"cached_tokens":10}}}`,
		``,
		`data: {"id":"chatcmpl_1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"completion_tokens":7,"completion_tokens_details":{"reasoning_tokens":3}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	response := decodeClosedChatUsageResponse(t, raw)
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

func TestChatUsageProjectionDoesNotFabricateUnknownBaseCounters(t *testing.T) {
	value := 7
	tests := []struct {
		name   string
		params canonical.TokenUsageParams
	}{
		{name: "input only", params: canonical.TokenUsageParams{InputTokens: &value}},
		{name: "output only", params: canonical.TokenUsageParams{OutputTokens: &value}},
		{name: "detail only", params: canonical.TokenUsageParams{ReasoningTokens: &value}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage, err := canonical.NewTokenUsage(test.params)
			if err != nil {
				t.Fatal(err)
			}
			if projected := chatUsageFromCanonical(usage); projected != nil {
				t.Fatalf("unrepresentable usage projected fabricated bases: %#v", projected)
			}
		})
	}
}

func decodeClosedChatUsageResponse(t *testing.T, raw string) *canonical.CanonicalResponse {
	t.Helper()
	reader := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_usage", nil)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	return response
}
