package messages

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestResponseDocumentEncoder_RefusalLowersToStopReason(t *testing.T) {
	t.Parallel()

	output := canonicaltest.Response(t, "msg_1", "claude-x", nil, "refusal")
	result, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, output)
	if err != nil {
		t.Fatalf("EncodeResponseDocument returned error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Document.Raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := payload["stop_reason"]; got != "refusal" {
		t.Fatalf("stop_reason = %#v, want refusal", got)
	}
}

func TestResponseStreamEncoder_RefusalLowersToStopReason(t *testing.T) {
	t.Parallel()

	events := canonical.SynthesizeResponseEnvelopeEvents("ex_msg", canonical.ResponseRef{SwobuID: "msg_1"}, "claude-x", nil, "refusal", canonical.NewUnknownTokenUsage())
	stream, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.CanonicalRequest{}, canonical.NewSliceEventReader(events), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatalf("EncodeResponseStream returned error: %v", err)
	}
	raw, err := io.ReadAll(stream.Stream.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, `"stop_reason":"refusal"`) {
		t.Fatalf("stream body missing refusal stop_reason: %s", body)
	}
}
