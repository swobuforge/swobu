package messages

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestEncodeHistoricalToolCallUsesStoredNamespacedKeyWithoutCurrentTools(t *testing.T) {
	key, _ := canonical.NewToolKey("remote/weather", canonical.ToolKindFunction, "lookup")
	callID, _ := canonical.NewToolCallID("call_1")
	arguments, _ := canonical.ParseJSONObject([]byte(`{"city":"London"}`))
	item, _ := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(arguments))
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}})
	names := testAttemptToolNames(request)
	want, _ := names.WireName(key)

	got, err := encodeMessagesToolCall(item, nil, names)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != want {
		t.Fatalf("encoded tool name = %q, want %q", got.Name, want)
	}
}
