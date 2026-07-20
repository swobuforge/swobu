package messages

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestMessagesStreamEncoder_EmitsSingleTextDeltaAndSingleMessageStop(t *testing.T) {
	t.Parallel()

	codec := legacyResponseStreamEncoder{}
	outputTokens := 2
	usage := mustTokenUsage(t, nil, &outputTokens, nil, nil)
	message, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("Hello world!")})
	events := canonical.SynthesizeResponseEnvelopeEvents("ex_1", canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_1")}, "m", []canonical.CanonicalItem{message}, "completed", usage)

	stream, err := codec.EncodeResponseStream(context.Background(), canonical.NewSliceEventReader(events), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatalf("EncodeResponseStream error: %v", err)
	}
	raw, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatalf("read stream error: %v", err)
	}
	parts := strings.Split(string(raw), "\n\n")
	frames := make([][]byte, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p) // swobu:io-string source=domain
		if p == "" {
			continue
		}
		frames = append(frames, []byte(p+"\n\n"))
	}

	types := frameTypes(t, frames)
	if got := countType(types, "content_block_delta"); got != 1 {
		t.Fatalf("content_block_delta count = %d, want 1; types=%v", got, types)
	}
	if got := countType(types, "message_start"); got != 1 {
		t.Fatalf("message_start count = %d, want 1; types=%v", got, types)
	}
	if got := countType(types, "message_delta"); got != 1 {
		t.Fatalf("message_delta count = %d, want 1; types=%v", got, types)
	}
	if got := countType(types, "message_stop"); got != 1 {
		t.Fatalf("message_stop count = %d, want 1; types=%v", got, types)
	}

	startPayload := firstPayloadByEventType(t, frames, "message_start")
	msg, ok := startPayload["message"].(map[string]any)
	if !ok {
		t.Fatalf("message_start payload shape invalid: %#v", startPayload)
	}
	if got, _ := msg["type"].(string); got != "message" {
		t.Fatalf("message_start message.type = %q, want %q", got, "message")
	}
	if got, _ := msg["role"].(string); got != "assistant" {
		t.Fatalf("message_start message.role = %q, want %q", got, "assistant")
	}
	if content, ok := msg["content"].([]any); !ok || len(content) != 0 {
		t.Fatalf("message_start message.content = %#v, want empty array", msg["content"])
	}

	deltaPayload := firstPayloadByEventType(t, frames, "content_block_delta")
	delta, ok := deltaPayload["delta"].(map[string]any)
	if !ok {
		t.Fatalf("delta payload shape invalid: %#v", deltaPayload)
	}
	if got, _ := delta["text"].(string); got != "Hello world!" {
		t.Fatalf("delta text = %q, want %q", got, "Hello world!")
	}

	messageDeltaPayload := firstPayloadByEventType(t, frames, "message_delta")
	messageDelta, ok := messageDeltaPayload["delta"].(map[string]any)
	if !ok {
		t.Fatalf("message_delta payload shape invalid: %#v", messageDeltaPayload)
	}
	if got, _ := messageDelta["stop_reason"].(string); got != "end_turn" {
		t.Fatalf("message_delta stop_reason = %q, want %q", got, "end_turn")
	}
	if usagePayload, ok := messageDeltaPayload["usage"].(map[string]any); !ok {
		t.Fatalf("message_delta usage shape invalid: %#v", messageDeltaPayload["usage"])
	} else if got := usagePayload["output_tokens"]; got != float64(2) {
		t.Fatalf("message_delta usage.output_tokens = %v, want 2", got)
	}
}

func mustTokenUsage(t *testing.T, input, output, cacheRead, cacheWrite *int) canonical.TokenUsage {
	t.Helper()
	usage, err := canonical.NewTokenUsageWithOptional(input, output, cacheRead, cacheWrite)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}
	return usage
}

func frameTypes(t *testing.T, frames [][]byte) []string {
	t.Helper()
	out := make([]string, 0, len(frames))
	for _, frame := range frames {
		ev, payload := decodeSSEFrame(t, frame)
		if ev == "" {
			continue
		}
		if typ, _ := payload["type"].(string); typ != "" {
			out = append(out, typ)
		}
	}
	return out
}

func firstPayloadByEventType(t *testing.T, frames [][]byte, wantType string) map[string]any {
	t.Helper()
	for _, frame := range frames {
		_, payload := decodeSSEFrame(t, frame)
		if typ, _ := payload["type"].(string); typ == wantType {
			return payload
		}
	}
	t.Fatalf("missing frame type %q", wantType)
	return nil
}

func decodeSSEFrame(t *testing.T, frame []byte) (string, map[string]any) {
	t.Helper()
	text := string(frame)
	var eventName, data string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line) // swobu:io-string source=domain
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:")) // swobu:io-string source=domain
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:")) // swobu:io-string source=domain
		}
	}
	payload := map[string]any{}
	if data != "" {
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			t.Fatalf("decode frame payload error: %v, frame=%q", err, text)
		}
	}
	return eventName, payload
}

func countType(types []string, want string) int {
	count := 0
	for _, typ := range types {
		if typ == want {
			count++
		}
	}
	return count
}
