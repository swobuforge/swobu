package canonical

import "testing"

func TestCanonicalItemTypedBranchesAreExclusiveAndClone(t *testing.T) {
	message, err := NewMessageItem(MessageRoleUser, []MessagePart{NewTextMessagePart("hello")})
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := NewToolCallID("call_1")
	object, _ := ParseJSONObject([]byte(`{"q":"swobu"}`))
	tool := testRequestToolKey(ToolKindFunction, "search")
	call, err := NewToolCallItem(callID, tool, NewJSONObjectToolInput(object))
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewToolResultItem(callID, []ToolResultPart{NewTextToolResultPart("done")}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []CanonicalItem{message, call, result} {
		if item.Clone().Kind() != item.Kind() {
			t.Fatalf("clone kind mismatch for %q", item.Kind())
		}
	}
	if _, ok := message.ToolCall(); ok {
		t.Fatal("message exposed tool-call branch")
	}
	if _, ok := call.Message(); ok {
		t.Fatal("tool call exposed message branch")
	}
	if _, ok := result.ToolCall(); ok {
		t.Fatal("tool result exposed tool-call branch")
	}
}
