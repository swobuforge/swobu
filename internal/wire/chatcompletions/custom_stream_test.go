package chatcompletions

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func TestChatStreamEncoderProjectsCustomToolInput(t *testing.T) {
	encoder := &chatCompletionsEnvelopeStreamEncoder{}
	if _, err := encoder.Encode(sse.StreamEvent{
		Kind: sse.StreamEventStarted, ResultID: "chat_1", Model: "m",
	}); err != nil {
		t.Fatal(err)
	}
	start, err := encoder.Encode(sse.StreamEvent{
		Kind:      sse.StreamEventItemStarted,
		ItemKind:  canonical.ItemKindToolCall,
		ItemID:    "item_0",
		ToolUseID: "call_1",
		Name:      "shell",
		ToolType:  canonical.ToolTypeCustom,
	})
	if err != nil {
		t.Fatal(err)
	}
	delta, err := encoder.Encode(sse.StreamEvent{
		Kind:           sse.StreamEventToolUseArgumentsDelta,
		ItemID:         "item_0",
		ArgumentsDelta: "echo hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(start[0]) + string(delta[0])
	for _, token := range []string{`"type":"custom"`, `"name":"shell"`, `"input":"echo hi"`} {
		if !strings.Contains(wire, token) {
			t.Fatalf("custom stream frames missing %s: %s", token, wire)
		}
	}
	if strings.Contains(wire, `"function"`) {
		t.Fatalf("custom stream was misclassified as a function: %s", wire)
	}
}
