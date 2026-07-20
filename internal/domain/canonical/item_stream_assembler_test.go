package canonical

import (
	"reflect"
	"strings"
	"testing"
)

func TestItemStartPayloadIsExclusiveAndDerived(t *testing.T) {
	callID, err := NewToolCallID("call_1")
	if err != nil {
		t.Fatal(err)
	}
	toolID := testRequestToolKey(ToolKindFunction, "weather")
	toolRef := toolID
	message := testMessageStart(MessageRoleAssistant)
	if message.Kind() != ItemKindMessage {
		t.Fatalf("message start kind = %q", message.Kind())
	}
	tool := testToolCallStart(callID, toolRef)
	if tool.Kind() != ItemKindToolCall {
		t.Fatalf("tool start kind = %q", tool.Kind())
	}
	invalid := ItemStartPayload{message: message.message, toolCall: tool.toolCall}
	if invalid.Kind() != "" {
		t.Fatalf("two-branch start kind = %q, want invalid", invalid.Kind())
	}
}

func TestItemStreamAssemblerReconstructsInterleavedCallsByOrdinal(t *testing.T) {
	first := testToolCallItem(t, "call_a", "alpha", `{"x":1}`)
	second := testToolCallItem(t, "call_b", "beta", `{"y":2}`)
	firstCall, _ := first.ToolCall()
	secondCall, _ := second.ToolCall()
	assembler := newItemStreamAssembler()
	events := []struct {
		kind  EventKind
		event ItemEvent
	}{
		{EventItemStart, ItemEvent{Position: ItemPosition{Item: 0}, Payload: testToolCallStart(firstCall.CallID(), firstCall.Tool())}},
		{EventItemStart, ItemEvent{Position: ItemPosition{Item: 1}, Payload: testToolCallStart(secondCall.CallID(), secondCall.Tool())}},
		{EventArgsDelta, ItemEvent{Position: ItemPosition{Item: 1}, Payload: ArgsDeltaPayload{Args: `{"y"`}}},
		{EventArgsDelta, ItemEvent{Position: ItemPosition{Item: 0}, Payload: ArgsDeltaPayload{Args: `{"x"`}}},
		{EventArgsDelta, ItemEvent{Position: ItemPosition{Item: 1}, Payload: ArgsDeltaPayload{Args: `:2}`}}},
		{EventArgsDelta, ItemEvent{Position: ItemPosition{Item: 0}, Payload: ArgsDeltaPayload{Args: `:1}`}}},
		{EventItemCompleted, ItemEvent{Position: ItemPosition{Item: 1}, Payload: ItemCompletedPayload{Item: second}}},
		{EventItemCompleted, ItemEvent{Position: ItemPosition{Item: 0}, Payload: ItemCompletedPayload{Item: first}}},
	}
	for _, event := range events {
		if err := assembler.apply(event.kind, event.event); err != nil {
			t.Fatalf("apply %s ordinal %d: %v", event.kind, event.event.Position.Item, err)
		}
	}
	items, err := assembler.completedItems()
	if err != nil {
		t.Fatal(err)
	}
	gotFirst, _ := items[0].ToolCall()
	gotSecond, _ := items[1].ToolCall()
	if gotFirst.CallID() != firstCall.CallID() || gotSecond.CallID() != secondCall.CallID() {
		t.Fatalf("completed order = %q, %q", gotFirst.CallID().String(), gotSecond.CallID().String())
	}
}

func TestItemStreamAssemblerRejectsIdentityAndArgumentMismatch(t *testing.T) {
	complete := testToolCallItem(t, "call_1", "weather", `{"location":"London"}`)
	call, _ := complete.ToolCall()
	otherCallID, _ := NewToolCallID("call_other")
	otherToolID := testRequestToolKey(ToolKindFunction, "other")
	tests := []struct {
		name  string
		start ItemStartPayload
		args  string
		want  string
	}{
		{"call id", testToolCallStart(otherCallID, call.Tool()), `{"location":"London"}`, "CallID"},
		{"tool id", testToolCallStart(call.CallID(), otherToolID), `{"location":"London"}`, "tool reference"},
		{"arguments", testToolCallStart(call.CallID(), call.Tool()), `{"location":"Paris"}`, "arguments"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assembler := newItemStreamAssembler()
			if err := assembler.apply(EventItemStart, ItemEvent{Position: ItemPosition{Item: 2}, Payload: test.start}); err != nil {
				t.Fatal(err)
			}
			if err := assembler.apply(EventArgsDelta, ItemEvent{Position: ItemPosition{Item: 2}, Payload: ArgsDeltaPayload{Args: test.args}}); err != nil {
				t.Fatal(err)
			}
			err := assembler.apply(EventItemCompleted, ItemEvent{Position: ItemPosition{Item: 2}, Payload: ItemCompletedPayload{Item: complete}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("completion error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestItemStreamAssemblerRejectsArgsBeforeStart(t *testing.T) {
	assembler := newItemStreamAssembler()
	if err := assembler.apply(EventArgsDelta, ItemEvent{Position: ItemPosition{Item: 3}, Payload: ArgsDeltaPayload{Args: "{"}}); err == nil {
		t.Fatal("args delta escaped before tool identity")
	}
}

func TestItemStreamAssemblerRejectsCompletedTextThatDiffersFromStream(t *testing.T) {
	completed, _ := NewMessageItem(MessageRoleAssistant, []MessagePart{NewTextMessagePart("goodbye")})
	assembler := newItemStreamAssembler()
	steps := []struct {
		kind  EventKind
		event ItemEvent
	}{
		{EventItemStart, ItemEvent{Position: ItemPosition{Item: 0}, Payload: testMessageStart(MessageRoleAssistant)}},
		{EventContentStart, ItemEvent{Position: ItemPosition{Item: 0, Part: 0}, Payload: ContentStartPayload{Kind: PartKindText}}},
		{EventTextDelta, ItemEvent{Position: ItemPosition{Item: 0, Part: 0}, Payload: TextDeltaPayload{Text: "hello"}}},
	}
	for _, step := range steps {
		if err := assembler.apply(step.kind, step.event); err != nil {
			t.Fatal(err)
		}
	}
	err := assembler.apply(EventItemCompleted, ItemEvent{Position: ItemPosition{Item: 0}, Payload: ItemCompletedPayload{Item: completed}})
	if err == nil || !strings.Contains(err.Error(), "streamed text") {
		t.Fatalf("completion error = %v, want streamed text mismatch", err)
	}
}

func TestItemStreamAssemblerRejectsTextWithoutContentStart(t *testing.T) {
	assembler := newItemStreamAssembler()
	if err := assembler.apply(EventItemStart, ItemEvent{Position: ItemPosition{Item: 0}, Payload: testMessageStart(MessageRoleAssistant)}); err != nil {
		t.Fatal(err)
	}
	if err := assembler.apply(EventTextDelta, ItemEvent{Position: ItemPosition{Item: 0, Part: 0}, Payload: TextDeltaPayload{Text: "escaped"}}); err == nil {
		t.Fatal("text delta escaped without a content start")
	}
}

func TestItemStreamAssemblerRejectsDuplicateItemAndPartOrdinals(t *testing.T) {
	assembler := newItemStreamAssembler()
	start := ItemEvent{Position: ItemPosition{Item: 0}, Payload: testMessageStart(MessageRoleAssistant)}
	if err := assembler.apply(EventItemStart, start); err != nil {
		t.Fatal(err)
	}
	if err := assembler.apply(EventItemStart, start); err == nil {
		t.Fatal("duplicate item ordinal was accepted")
	}
	part := ItemEvent{Position: ItemPosition{Item: 0, Part: 0}, Payload: ContentStartPayload{Kind: PartKindText}}
	if err := assembler.apply(EventContentStart, part); err != nil {
		t.Fatal(err)
	}
	if err := assembler.apply(EventContentStart, part); err == nil {
		t.Fatal("duplicate part ordinal was accepted")
	}
}

func TestItemStreamAssemblerRejectsCompletedPartTopologyMismatch(t *testing.T) {
	completed, _ := NewMessageItem(MessageRoleAssistant, []MessagePart{
		NewTextMessagePart("first"),
		NewTextMessagePart("second"),
	})
	assembler := newItemStreamAssembler()
	if err := assembler.apply(EventItemStart, ItemEvent{Position: ItemPosition{Item: 0}, Payload: testMessageStart(MessageRoleAssistant)}); err != nil {
		t.Fatal(err)
	}
	if err := assembler.apply(EventContentStart, ItemEvent{Position: ItemPosition{Item: 0, Part: 0}, Payload: ContentStartPayload{Kind: PartKindText}}); err != nil {
		t.Fatal(err)
	}
	if err := assembler.apply(EventTextDelta, ItemEvent{Position: ItemPosition{Item: 0, Part: 0}, Payload: TextDeltaPayload{Text: "first"}}); err != nil {
		t.Fatal(err)
	}
	err := assembler.apply(EventItemCompleted, ItemEvent{Position: ItemPosition{Item: 0}, Payload: ItemCompletedPayload{Item: completed}})
	if err == nil || !strings.Contains(err.Error(), "part count") {
		t.Fatalf("completion error = %v, want part-count mismatch", err)
	}
}

func TestEventMetadataCarriesNoToolOrCallIdentity(t *testing.T) {
	typeOf := reflect.TypeOf(EventMetadataFields{})
	for _, forbidden := range []string{"ToolID", "CallID", "ToolUseID", "Name"} {
		if _, ok := typeOf.FieldByName(forbidden); ok {
			t.Fatalf("EventMetadataFields contains forbidden %s", forbidden)
		}
	}
}

func testToolCallItem(t *testing.T, rawCallID, rawToolID, rawArgs string) CanonicalItem {
	t.Helper()
	callID, err := NewToolCallID(rawCallID)
	if err != nil {
		t.Fatal(err)
	}
	object, err := ParseJSONObject([]byte(rawArgs))
	if err != nil {
		t.Fatal(err)
	}
	tool := testRequestToolKey(ToolKindFunction, rawToolID)
	item, err := NewToolCallItem(callID, tool, NewJSONObjectToolInput(object))
	if err != nil {
		t.Fatal(err)
	}
	return item
}
