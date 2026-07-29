package chatcompletions

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponseBuffered_ContentFilterPreservesTerminalReason(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id":"chatcmpl_1",
		"model":"m",
		"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"content_filter","content_filter_result":{"error":{"code":"content_filter","message":"ResponsibleAI result indicated block action."}}}],
		"usage":{"prompt_tokens":12,"completion_tokens":0}
	}`)

	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex_content_filter", &recordingDecisionSink{})
	if err != nil {
		t.Fatalf("decodeResponseBuffered returned error: %v", err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	out, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	if got := out.Completion().Reason(); got != "content_filter" {
		t.Fatalf("finish reason = %q, want content_filter", got)
	}
}

func TestDecodeResponseStream_ContentFilterPreservesTerminalReason(t *testing.T) {
	t.Parallel()

	raw := "data: {\"created\":1781902359,\"id\":\"chatcmpl-b1a3544fdfaf41e3a3e812af05b1e\",\"model\":\"m\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{},\"finish_reason\":\"content_filter\",\"content_filter_result\":{\"error\":{\"code\":\"content_filter\",\"message\":\"ResponsibleAI result indicated block action.\"}}}]}\n\n"

	sink := &recordingDecisionSink{}
	reader := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_stream_content_filter", sink)
	defer func() { _ = reader.Close(context.Background()) }()

	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	out, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	if got := out.Completion().Reason(); got != "content_filter" {
		t.Fatalf("finish reason = %q, want content_filter", got)
	}
}
