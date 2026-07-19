package canonical

import (
	"fmt"
	"strings"
	"testing"
)

func TestCanonicalItemTypedArmsAreExclusive(t *testing.T) {
	text := NewTextItem(ItemAuthorUser, "hello")
	if payload, ok := text.TextItem(); !ok || payload.Text != "hello" {
		t.Fatalf("text arm = %#v, %v", payload, ok)
	}
	if _, ok := text.ToolUse(); ok {
		t.Fatal("text item exposed tool-use arm")
	}
	if _, ok := text.ToolResult(); ok {
		t.Fatal("text item exposed tool-result arm")
	}

	toolUse := NewToolUseItem(ItemAuthorAssistant, "item_1", "call_1", "search", NewToolArgumentsObject(`{"q":"swobu"}`))
	if payload, ok := toolUse.ToolUse(); !ok || payload.UseID != "call_1" {
		t.Fatalf("tool-use arm = %#v, %v", payload, ok)
	}
	if _, ok := toolUse.TextItem(); ok {
		t.Fatal("tool-use item exposed text arm")
	}

	toolResult := NewToolResultItem(ItemAuthorTool, "call_1", "done")
	if payload, ok := toolResult.ToolResult(); !ok || payload.Text != "done" {
		t.Fatalf("tool-result arm = %#v, %v", payload, ok)
	}
	if _, ok := toolResult.ToolUse(); ok {
		t.Fatal("tool-result item exposed tool-use arm")
	}
}

func TestProjectRequestRejectsArgsDeltaAgainstTextItem(t *testing.T) {
	envelope := &ClosedEnvelope{Kind: EnvRequest, Events: []Event{
		{Kind: EventEnvelopeStart, EnvID: "message_1", Payload: EnvelopeStartPayload{Kind: EnvMessage, Role: ItemAuthorUser}},
		{Kind: EventArgsDelta, EnvID: "message_1", Payload: ArgsDeltaPayload{Args: `{}`}},
	}}
	if _, err := envelope.ProjectRequest(); err == nil || !strings.Contains(err.Error(), "args delta") {
		t.Fatalf("ProjectRequest error = %v, want args-delta arm failure", err)
	}
}

func TestProjectRequestRejectsTextDeltaAgainstToolUseItem(t *testing.T) {
	envelope := &ClosedEnvelope{Kind: EnvRequest, Events: []Event{
		{Kind: EventEnvelopeStart, EnvID: "tool_1", Payload: EnvelopeStartPayload{Kind: EnvToolCall, Role: ItemAuthorAssistant, ToolUseID: "call_1", Name: "search"}},
		{Kind: EventTextDelta, EnvID: "tool_1", Payload: TextDeltaPayload{Text: "wrong arm"}},
	}}
	if _, err := envelope.ProjectRequest(); err == nil || !strings.Contains(err.Error(), "text delta") {
		t.Fatalf("ProjectRequest error = %v, want text-delta arm failure", err)
	}
}

func TestProjectRequestRejectsDeltaForUnknownCanonicalItem(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event Event
	}{
		{
			name:  "text delta",
			event: Event{Kind: EventTextDelta, EnvID: "missing_text", Payload: TextDeltaPayload{Text: "orphaned"}},
		},
		{
			name:  "args delta",
			event: Event{Kind: EventArgsDelta, EnvID: "missing_args", Payload: ArgsDeltaPayload{Args: `{}`}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			envelope := &ClosedEnvelope{Kind: EnvRequest, Events: []Event{tc.event}}
			_, err := envelope.ProjectRequest()
			want := fmt.Sprintf("%s targets unknown canonical item %q", tc.event.Kind, tc.event.EnvID)
			if err == nil || err.Error() != want {
				t.Fatalf("ProjectRequest error = %v, want %q", err, want)
			}
		})
	}
}

func TestCanonicalItemClonePreservesEveryPayloadArm(t *testing.T) {
	items := []CanonicalItem{
		NewTextItem(ItemAuthorUser, "text"),
		NewToolUseItem(ItemAuthorAssistant, "item_1", "call_1", "search", NewToolArgumentsObject(`{"q":"x"}`)),
		NewToolResultItem(ItemAuthorTool, "call_1", "result"),
	}
	for _, item := range items {
		clone := item.Clone()
		if clone.Kind() != item.Kind() {
			t.Fatalf("clone kind = %q, want %q", clone.Kind(), item.Kind())
		}
	}
}
