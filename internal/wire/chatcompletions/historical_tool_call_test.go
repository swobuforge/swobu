package chatcompletions

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestEncodeHistoricalToolCallUsesStoredNamespacedKeyWithoutCurrentTools(t *testing.T) {
	key, _ := canonical.NewToolKey("remote/weather", canonical.ToolKindFunction, "lookup")
	callID, _ := canonical.NewToolCallID("call_1")
	arguments, _ := canonical.ParseJSONObject([]byte(`{"city":"London"}`))
	item, _ := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(arguments))
	call, _ := item.ToolCall()
	want := key.Name()

	got, err := encodeChatToolCall(call, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Function == nil || got.Function.Name != want {
		t.Fatalf("encoded tool name = %#v, want %q", got.Function, want)
	}
}
