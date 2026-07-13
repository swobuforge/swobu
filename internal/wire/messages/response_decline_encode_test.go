package messages

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestResponseDocumentEncoder_RefusalLowersToStopReason(t *testing.T) {
	t.Parallel()

	output := canonical.NewConversationOutput("msg_1", "claude-x", nil, "refusal")
	result, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(output)
	if err != nil {
		t.Fatalf("EncodeResponseDocument returned error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Value.Raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := payload["stop_reason"]; got != "refusal" {
		t.Fatalf("stop_reason = %#v, want refusal", got)
	}
}

func TestResponseStreamEncoder_RefusalLowersToStopReason(t *testing.T) {
	t.Parallel()

	events := canonical.SynthesizeResponseEnvelopeEvents("ex_msg", "msg_1", "claude-x", nil, "refusal", canonical.NewUnknownTokenUsage())
	stream, err := (ResponseStreamEncoder{}).EncodeResponseStream(canonical.NewSliceEventReader(events), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatalf("EncodeResponseStream returned error: %v", err)
	}
	raw, err := io.ReadAll(carrier.ReadCloserFromFrameReader(stream.Value.Frames))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, `"stop_reason":"refusal"`) {
		t.Fatalf("stream body missing refusal stop_reason: %s", body)
	}
}
