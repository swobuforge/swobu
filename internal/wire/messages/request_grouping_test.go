package messages

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func TestEncodeItemsGroupsMaximalAssistantOwnedSequence(t *testing.T) {
	callID, _ := canonical.NewToolCallID("call_1")
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	decl := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "weather"), "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())
	input, _ := canonical.ParseJSONObject([]byte(`{"city":"London"}`))
	before, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("before")})
	call, _ := canonical.NewToolCallItem(callID, decl.Key(), canonical.NewJSONObjectToolInput(input))
	after, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("after")})

	for _, currentTools := range [][]canonical.ToolDeclaration{
		nil,
		{canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "other"), "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())},
	} {
		messages, err := encodeItems([]canonical.CanonicalItem{before, call, after}, currentTools, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 || messages[0].Role != "assistant" || len(messages[0].Content) != 3 {
			t.Fatalf("grouped messages = %#v", messages)
		}
		if messages[0].Content[0].Text != "before" || messages[0].Content[1].Type != "tool_use" || messages[0].Content[1].Name != "weather" || messages[0].Content[2].Text != "after" {
			t.Fatalf("assistant block order = %#v", messages[0].Content)
		}
		raw, _ := json.Marshal(messages[0].Content)
		decoded, _, err := decodeMessagesItems(raw, 0, "assistant", []canonical.ToolDeclaration{decl}, nil, shared.ImageDecodeLimitPolicy{}, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(decoded) != 3 || decoded[0].Kind() != canonical.ItemKindMessage || decoded[1].Kind() != canonical.ItemKindToolCall || decoded[2].Kind() != canonical.ItemKindMessage {
			t.Fatalf("round-trip items = %#v", decoded)
		}
	}
}

func TestEncodeItemsPreservesMultipleToolCallsBeforeAssistantText(t *testing.T) {
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	declA := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "a"), "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())
	declB := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "b"), "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())
	input, _ := canonical.ParseJSONObject([]byte(`{}`))
	callIDA, _ := canonical.NewToolCallID("call_a")
	callIDB, _ := canonical.NewToolCallID("call_b")
	callA, _ := canonical.NewToolCallItem(callIDA, declA.Key(), canonical.NewJSONObjectToolInput(input))
	callB, _ := canonical.NewToolCallItem(callIDB, declB.Key(), canonical.NewJSONObjectToolInput(input))
	message, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("done")})

	messages, err := encodeItems([]canonical.CanonicalItem{callA, callB, message}, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || len(messages[0].Content) != 3 || messages[0].Content[0].Name != "a" || messages[0].Content[1].Name != "b" || messages[0].Content[2].Text != "done" {
		t.Fatalf("assistant block order = %#v", messages)
	}
}

func TestEncodeItemsGroupsToolResultWithFollowingUserText(t *testing.T) {
	callID, _ := canonical.NewToolCallID("call_1")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("sunny")}, false)
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("thanks")})

	messages, err := encodeItems([]canonical.CanonicalItem{result, message}, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != "user" || len(messages[0].Content) != 2 {
		t.Fatalf("grouped messages = %#v", messages)
	}
	if messages[0].Content[0].Type != "tool_result" || messages[0].Content[1].Text != "thanks" {
		t.Fatalf("user block order = %#v", messages[0].Content)
	}
}
