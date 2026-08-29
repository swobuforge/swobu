package chatcompletions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestEncodeHistoricalToolCallUsesStoredNamespacedKeyWithoutCurrentTools(t *testing.T) {
	key, _ := canonical.NewToolKey("remote/weather", canonical.ToolKindFunction, "lookup")
	callID, _ := canonical.NewToolCallID("call_1")
	arguments, _ := canonical.ParseJSONObject([]byte(`{"city":"London"}`))
	item, _ := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(arguments))
	call, _ := item.ToolCall()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}})
	names := testAttemptToolNames(request)
	want, _ := names.WireName(key)

	projection := ToolProjection{ProjectCall: func(call canonical.ToolCallItem) (toolCallBody, error) {
		object, _ := call.Input().Object()
		return toolCallBody{ID: call.CallID().String(), Type: "function", Function: &toolFunctionBody{Name: want, Arguments: object.String()}}, nil
	}}
	got, err := encodeChatToolCall(call, compiledToolProjection{
		lowered:     wire.LoweredToolSet{Records: []wire.LoweredToolRecord{{Key: key, Kind: key.Kind(), FragmentCount: 1, TargetType: "function", TargetName: want}}},
		occurrences: map[canonical.ToolKey]ToolProjection{key: projection},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Function == nil || got.Function.Name != want {
		t.Fatalf("encoded tool name = %#v, want %q", got.Function, want)
	}
}

func TestEncodeHistoricalToolCallRejectsMissingEmittedIdentity(t *testing.T) {
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "lookup")
	callID, _ := canonical.NewToolCallID("call_1")
	arguments, _ := canonical.ParseJSONObject([]byte(`{}`))
	item, _ := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(arguments))
	call, _ := item.ToolCall()

	if _, err := encodeChatToolCall(call, compiledToolProjection{}); err == nil {
		t.Fatal("historical Chat tool call without emitted identity was accepted")
	}
}

func TestCustomSlotAloneOwnsWeirdChatDeclarationCallAndPolicy(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "canonical-shell")
	tool := canonicaltest.MustCustomTool(key, "Run raw text", canonical.EmptyToolFormat())
	call := canonicaltest.ToolCall(t, "call_weird", key, canonical.NewTextToolInput("abc"))
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, tool), call,
		}, ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &key)),
	})
	weirdCustom := func(_ ToolLoweringContext, _ canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
		parameters := json.RawMessage(`{"type":"object","properties":{"payload":{"type":"string"}},"required":["payload"]}`)
		encoded := ProviderRequestTool{Type: "function", Function: &chatCompletionsToolDefinitionFunctionDTO{Name: "x", Parameters: parameters}}
		return FunctionProjection(encoded, func(call canonical.ToolCallItem) (string, error) {
			text, ok := call.Input().Text()
			if !ok {
				return "", canonical.BadRequest("weird Custom requires text")
			}
			raw, err := json.Marshal(map[string]string{"payload": text})
			return string(raw), err
		}), nil, nil
	}
	document, err := CompileProviderRequestDocument(request, testAttemptToolNames(request), delivery.BufferedDelivery(), nil, "", CompileOptions{
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
	for _, want := range []string{`"name":"x"`, `"arguments":"{\"payload\":\"abc\"}"`, `"tool_choice":{"function":{"name":"x"},"type":"function"}`} {
		if !strings.Contains(wire, want) {
			t.Fatalf("weird Chat Custom projection = %s, want %s", wire, want)
		}
	}
}
