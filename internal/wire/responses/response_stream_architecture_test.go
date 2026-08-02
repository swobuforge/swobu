package responses

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestResponsesBufferedAndStreamedMessageSemanticsConverge(t *testing.T) {
	bufferedRaw := []byte(`{"id":"resp_1","model":"m","status":"completed","output":[{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"hello"}]}]}`)
	bufferedStream, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, bufferedRaw, "buffered", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	buffered := projectResponsesStream(t, bufferedStream)

	streamRaw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n\n" +
		responsesCompletedFrame("[]", "")
	streamed := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, streamRaw)

	assertResponsesMessageTexts(t, buffered.Items(), "hello")
	assertResponsesMessageTexts(t, streamed.Items(), "hello")
}

func TestResponsesTerminalFormsHaveOneTerminalOutcome(t *testing.T) {
	itemDone := "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n\n"
	completed := responsesCompletedFrame("[]", "")
	tests := []struct {
		name       string
		terminal   string
		wantStatus canonical.EnvelopeStatus
	}{
		{name: "done sentinel", terminal: "data: [DONE]\n\n", wantStatus: canonical.EnvelopeStatusCompleted},
		{name: "response completed", terminal: completed, wantStatus: canonical.EnvelopeStatusCompleted},
		{name: "duplicate response completed", terminal: completed + completed, wantStatus: canonical.EnvelopeStatusCompleted},
		{name: "eof", terminal: "", wantStatus: canonical.EnvelopeStatusError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := responsesCreatedFrame() + itemDone + test.terminal
			stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
			ends := 0
			var status canonical.EnvelopeStatus
			for {
				event, err := stream.Next(context.Background())
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if event.Kind != canonical.EventEnvelopeEnd {
					continue
				}
				end, ok := event.Payload.(canonical.EnvelopeEndPayload)
				if ok && end.Kind == canonical.EnvResponse {
					ends++
					status = end.Status
				}
			}
			if ends != 1 || status != test.wantStatus {
				t.Fatalf("terminal outcomes = %d status=%q, want one %q", ends, status, test.wantStatus)
			}
		})
	}
}

func BenchmarkResponsesStreamCheckpointRetention(b *testing.B) {
	for _, count := range []int{1, 32} {
		b.Run(fmt.Sprintf("items_%d", count), func(b *testing.B) {
			var frames strings.Builder
			frames.WriteString(responsesCreatedFrame())
			for index := range count {
				fmt.Fprintf(&frames, "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":{\"type\":\"message\",\"id\":\"msg_%d\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n\n", index, index)
			}
			frames.WriteString(responsesCompletedFrame("[]", ""))
			raw := frames.String()
			b.ReportAllocs()
			for b.Loop() {
				stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "bench", nil, true)
				if _, err := canonical.ReadClosedEnvelope(context.Background(), stream, canonical.EnvResponse); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func projectResponsesStream(t *testing.T, stream canonical.ResponseStream) *canonical.CanonicalResponse {
	t.Helper()
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "swobu_test"})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), bound, canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	return response
}
