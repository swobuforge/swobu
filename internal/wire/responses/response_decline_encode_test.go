package responses

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestResponseDocumentEncoder_ContentFilterLowersToIncomplete(t *testing.T) {
	t.Parallel()

	output := canonical.NewConversationOutput("resp_1", "m", nil, "content_filter")
	result, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(output)
	if err != nil {
		t.Fatalf("EncodeResponseDocument returned error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Document.Raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := payload["status"]; got != "incomplete" {
		t.Fatalf("status = %#v, want incomplete", got)
	}
	details, ok := payload["incomplete_details"].(map[string]any)
	if !ok {
		t.Fatalf("incomplete_details = %#v, want object", payload["incomplete_details"])
	}
	if got := details["reason"]; got != "content_filter" {
		t.Fatalf("incomplete_details.reason = %#v, want content_filter", got)
	}
}

func TestResponseStreamEncoder_ContentFilterLowersToIncomplete(t *testing.T) {
	t.Parallel()

	events := canonical.SynthesizeResponseEnvelopeEvents("ex_resp", canonical.ResponseRef{SwobuID: "resp_1"}, "m", nil, "content_filter", canonical.NewUnknownTokenUsage())
	stream, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.NewSliceEventReader(events), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatalf("EncodeResponseStream returned error: %v", err)
	}
	defer func() { _ = stream.Stream.Body.Close() }()

	raw, err := io.ReadAll(stream.Stream.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, `"type":"response.incomplete"`) {
		t.Fatalf("stream body missing response.incomplete: %s", body)
	}
	if !strings.Contains(body, `"reason":"content_filter"`) {
		t.Fatalf("stream body missing incomplete_details.reason content_filter: %s", body)
	}
}
