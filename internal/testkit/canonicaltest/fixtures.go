package canonicaltest

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func MustMessage(role canonical.MessageRole, text string) canonical.CanonicalItem {
	item, err := canonical.NewMessageItem(role, []canonical.MessagePart{canonical.NewTextMessagePart(text)})
	if err != nil {
		panic(err)
	}
	return item
}

func Message(t testing.TB, role canonical.MessageRole, text string) canonical.CanonicalItem {
	t.Helper()
	item, err := canonical.NewMessageItem(role, []canonical.MessagePart{canonical.NewTextMessagePart(text)})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func Response(t testing.TB, id, model string, items []canonical.CanonicalItem, finish string) canonical.CanonicalResponse {
	return ResponseWithUsage(t, id, model, items, finish, canonical.NewUnknownTokenUsage())
}

// MustResponse is for test doubles whose interface does not carry testing.TB.
func MustResponse(id, model string, items []canonical.CanonicalItem, finish string) canonical.CanonicalResponse {
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID(id)}, model, items, finish, canonical.NewUnknownTokenUsage())
	if err != nil {
		panic(err)
	}
	return response
}

func ResponseWithUsage(t testing.TB, id, model string, items []canonical.CanonicalItem, finish string, usage canonical.TokenUsage) canonical.CanonicalResponse {
	return ResponseWithRef(t, canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID(id)}, model, items, finish, usage)
}

func ResponseWithRef(t testing.TB, responseRef canonical.ResponseRef, model string, items []canonical.CanonicalItem, finish string, usage canonical.TokenUsage) canonical.CanonicalResponse {
	t.Helper()
	response, err := canonical.NewCanonicalResponse(responseRef, model, items, finish, usage)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func Schema(t testing.TB, raw string) canonical.ToolSchema {
	t.Helper()
	object, err := canonical.ParseJSONObject([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewToolSchemaObject(object)
}

func MustSchema(raw string) canonical.ToolSchema {
	object, err := canonical.ParseJSONObject([]byte(raw))
	if err != nil {
		panic(err)
	}
	return canonical.NewToolSchemaObject(object)
}

func Object(t testing.TB, raw string) canonical.JSONObject {
	t.Helper()
	object, err := canonical.ParseJSONObject([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func MustToolFormat(raw string) canonical.ToolFormat {
	object, err := canonical.ParseJSONObject([]byte(raw))
	if err != nil {
		panic(err)
	}
	return canonical.NewToolFormatObject(object)
}

func SpecifiedToolSet(t testing.TB, declarations ...canonical.ToolDeclaration) canonical.Specified[canonical.ToolSet] {
	return canonical.Specify(ToolSet(t, declarations...))
}

func MustInstruction(role canonical.MessageRole, text string) canonical.Instruction {
	instruction, err := canonical.NewInstruction(role, text)
	if err != nil {
		panic(err)
	}
	return instruction
}

func InstructionSetText(set canonical.InstructionSet) string {
	var out strings.Builder
	for index, instruction := range set.Instructions() {
		if index > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(instruction.Text())
	}
	return out.String()
}

func FunctionTool(t testing.TB, name string, schema canonical.ToolSchema) canonical.ToolDeclaration {
	t.Helper()
	key, err := canonical.NewRequestToolKey(canonical.ToolKindFunction, name)
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := canonical.NewFunctionTool(key, "", schema, canonical.Unspecified[bool]())
	if err != nil {
		t.Fatal(err)
	}
	return declaration
}

func ToolSet(t testing.TB, declarations ...canonical.ToolDeclaration) canonical.ToolSet {
	t.Helper()
	set, err := canonical.NewToolSet(declarations)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func ToolCall(t testing.TB, callID string, key canonical.ToolKey, input canonical.ToolInput) canonical.CanonicalItem {
	t.Helper()
	id, err := canonical.NewToolCallID(callID)
	if err != nil {
		t.Fatal(err)
	}
	item, err := canonical.NewToolCallItem(id, key, input)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
