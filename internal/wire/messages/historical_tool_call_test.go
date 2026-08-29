package messages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestEncodeHistoricalToolCallUsesStoredNamespacedKeyWithoutCurrentTools(t *testing.T) {
	key, _ := canonical.NewToolKey("remote/weather", canonical.ToolKindFunction, "lookup")
	callID, _ := canonical.NewToolCallID("call_1")
	arguments, _ := canonical.ParseJSONObject([]byte(`{"city":"London"}`))
	item, _ := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(arguments))
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}})
	names := testAttemptToolNames(request)
	want, _ := names.WireName(key)

	projection := ToolProjection{ProjectCall: func(call canonical.ToolCallItem) (ToolCallProjection, error) {
		object, _ := call.Input().Object()
		return ToolCallProjection{Type: "tool_use", Name: want, Input: json.RawMessage(object.Bytes())}, nil
	}}
	got, err := encodeMessagesToolCall(item, compiledToolProjection{
		lowered:     wire.LoweredToolSet{Records: []wire.LoweredToolRecord{{Key: key, Kind: key.Kind(), FragmentCount: 1, TargetType: "tool", TargetName: want}}},
		occurrences: map[canonical.ToolKey]ToolProjection{key: projection},
	})
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

func TestCustomSlotAloneOwnsWeirdMessagesDeclarationCallResultAndPolicy(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "canonical-shell")
	tool := canonicaltest.MustCustomTool(key, "Run raw text", canonical.EmptyToolFormat())
	call := canonicaltest.ToolCall(t, "call_weird", key, canonical.NewTextToolInput("abc"))
	callValue, _ := call.ToolCall()
	result, err := canonical.NewToolResultItem(callValue.CallID(), []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, tool), call, result,
		}, ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &key)),
	})
	weirdCustom := func(_ ToolLoweringContext, _ canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
		fragment := ProviderRequestTool{Name: "x", InputSchema: json.RawMessage(`{"type":"object","properties":{"payload":{"type":"string"}},"required":["payload"]}`)}
		projection := CallableProjection(fragment)
		projection.ProjectCall = func(call canonical.ToolCallItem) (ToolCallProjection, error) {
			text, ok := call.Input().Text()
			if !ok {
				return ToolCallProjection{}, canonical.BadRequest("weird Custom requires text")
			}
			raw, err := json.Marshal(map[string]string{"payload": text})
			return ToolCallProjection{Type: "tool_use", Name: "x", Input: raw}, err
		}
		return projection, nil, nil
	}
	names := testAttemptToolNames(request)
	document, err := CompileProviderRequestDocument(request, names, delivery.BufferedDelivery(), nil, "", CompileOptions{
		Lowering: DefaultLowering().Overlay(Lowering{Tools: ToolLowering{Custom: weirdCustom}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeProviderRequestDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(encoded.RawBytes())
	for _, want := range []string{`"name":"x"`, `"input":{"payload":"abc"}`, `"type":"tool_result"`, `"tool_choice":{"name":"x","type":"tool"}`} {
		if !strings.Contains(wire, want) {
			t.Fatalf("weird Messages Custom projection = %s, want %s", wire, want)
		}
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
