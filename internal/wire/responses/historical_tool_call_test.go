package responses

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestEncodeHistoricalToolCallUsesStoredNamespacedKeyWithoutCurrentTools(t *testing.T) {
	key, _ := canonical.NewToolKey("remote/weather", canonical.ToolKindFunction, "lookup")
	callID, _ := canonical.NewToolCallID("call_1")
	arguments, _ := canonical.ParseJSONObject([]byte(`{"city":"London"}`))
	item, _ := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(arguments))
	want := key.Name()

	encoded, err := encodeConversation([]canonical.CanonicalItem{item}, nil, compat.CompatibilityPolicy{}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := encoded[0].(functionCallItem)
	if !ok || got.Name != want {
		t.Fatalf("encoded tool call = %#v, want name %q", encoded[0], want)
	}
}
