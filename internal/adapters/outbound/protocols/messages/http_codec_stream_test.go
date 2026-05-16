package messages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestMessagesStreamEncoder_EmitsSingleTextDeltaAndSingleMessageStop(t *testing.T) {
	t.Parallel()

	codec := MessagesCodec{}
	encoder := codec.NewStreamState()
	events := []canonical.Event{
		{
			ExchangeID: "ex_1",
			Seq:        1,
			Kind:       canonical.EventEnvelopeStart,
			EnvID:      "res_1",
			Payload:    canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse},
		},
		{
			ExchangeID: "ex_1",
			Seq:        2,
			Kind:       canonical.EventEnvelopeStart,
			EnvID:      "msg_1",
			ParentID:   "res_1",
			Payload:    canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant},
			Meta:       canonical.EventMeta{NativeID: "text_0"},
		},
		{
			ExchangeID: "ex_1",
			Seq:        3,
			Kind:       canonical.EventTextDelta,
			EnvID:      "msg_1",
			ParentID:   "res_1",
			Payload:    canonical.TextDeltaPayload{Text: "Hello world!"},
		},
		{
			ExchangeID: "ex_1",
			Seq:        4,
			Kind:       canonical.EventEnvelopeEnd,
			EnvID:      "msg_1",
			ParentID:   "res_1",
			Payload:    canonical.EnvelopeEndPayload{Kind: canonical.EnvMessage, Status: canonical.EnvelopeStatusCompleted},
		},
		{
			ExchangeID: "ex_1",
			Seq:        5,
			Kind:       canonical.EventUsage,
			EnvID:      "res_1",
			Payload:    canonical.UsagePayload{Usage: canonical.NewUnknownTokenUsage()},
		},
		{
			ExchangeID: "ex_1",
			Seq:        6,
			Kind:       canonical.EventFinish,
			EnvID:      "res_1",
			Payload:    canonical.FinishPayload{Reason: "completed"},
		},
		{
			ExchangeID: "ex_1",
			Seq:        7,
			Kind:       canonical.EventEnvelopeEnd,
			EnvID:      "res_1",
			Payload:    canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted},
		},
	}

	frames := make([][]byte, 0, 8)
	for _, ev := range events {
		emitted, err := encoder.EncodeEnvelopeEvent(ev)
		if err != nil {
			t.Fatalf("EncodeEnvelopeEvent error: %v", err)
		}
		frames = append(frames, emitted...)
	}

	types := frameTypes(t, frames)
	if got := countType(types, "content_block_delta"); got != 1 {
		t.Fatalf("content_block_delta count = %d, want 1; types=%v", got, types)
	}
	if got := countType(types, "message_stop"); got != 1 {
		t.Fatalf("message_stop count = %d, want 1; types=%v", got, types)
	}

	deltaPayload := firstPayloadByEventType(t, frames, "content_block_delta")
	delta, ok := deltaPayload["delta"].(map[string]any)
	if !ok {
		t.Fatalf("delta payload shape invalid: %#v", deltaPayload)
	}
	if got, _ := delta["text"].(string); got != "Hello world!" {
		t.Fatalf("delta text = %q, want %q", got, "Hello world!")
	}
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
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
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

