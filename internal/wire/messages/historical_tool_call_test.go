package messages

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
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

func TestMessagesOmitsUnloweredCustomCallAndResultAtomically(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "shell")
	tool := canonicaltest.MustCustomTool(key, "", canonicaltest.MustToolFormat(`{"type":"text"}`))
	callID, _ := canonical.NewToolCallID("call_custom")
	call, _ := canonical.NewToolCallItem(callID, key, canonical.NewTextToolInput("echo exact"))
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("ok")}, false)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, tool),
			canonicaltest.Message(t, canonical.MessageRoleUser, "before"),
			call, result,
			canonicaltest.Message(t, canonical.MessageRoleUser, "continue"),
		},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	var changes []compat.Change
	document, err := EncodeCarrierWithChanges(request, names, delivery.BufferedDelivery(), &changes, "")
	if err != nil {
		t.Fatal(err)
	}
	raw := string(document.RawBytes())
	if strings.Contains(raw, "call_custom") || strings.Contains(raw, "echo exact") || strings.Contains(raw, `"ok"`) {
		t.Fatalf("unlowered Custom effect leaked into Messages: %s", raw)
	}
	if !strings.Contains(raw, "continue") {
		t.Fatalf("current conversation was lost: %s", raw)
	}
	wantTool := compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(key))
	wantEffect := compat.NewOmission(canonical.RequestItemsKind, canonical.RequestItemOccurrence(2))
	if !containsChange(changes, wantTool) || !containsChange(changes, wantEffect) {
		t.Fatalf("changes = %#v, want tool %#v and effect %#v", changes, wantTool, wantEffect)
	}
}

func containsChange(changes []compat.Change, want compat.Change) bool {
	for _, change := range changes {
		if change == want {
			return true
		}
	}
	return false
}
