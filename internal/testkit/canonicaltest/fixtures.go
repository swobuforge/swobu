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

func Response(t testing.TB, id, model string, items []canonical.CanonicalItem, completion canonical.Completion) canonical.CanonicalResponse {
	return ResponseWithUsage(t, id, model, items, completion, canonical.NewUnknownTokenUsage())
}

// MustResponse is for test doubles whose interface does not carry testing.TB.
func MustResponse(id, model string, items []canonical.CanonicalItem, completion canonical.Completion) canonical.CanonicalResponse {
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID(id)}, model, items, completion, canonical.NewUnknownTokenUsage())
	if err != nil {
		panic(err)
	}
	return response
}

func ResponseWithUsage(t testing.TB, id, model string, items []canonical.CanonicalItem, completion canonical.Completion, usage canonical.TokenUsage) canonical.CanonicalResponse {
	return ResponseWithRef(t, canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID(id)}, model, items, completion, usage)
}

func ResponseWithRef(t testing.TB, responseRef canonical.ResponseRef, model string, items []canonical.CanonicalItem, completion canonical.Completion, usage canonical.TokenUsage) canonical.CanonicalResponse {
	t.Helper()
	response, err := canonical.NewCanonicalResponse(responseRef, model, items, completion, usage)
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

func ToolDeclarations(t testing.TB, declarations ...canonical.ToolDeclaration) canonical.CanonicalItem {
	t.Helper()
	item, err := canonical.NewToolDeclarationsItem(ToolSet(t, declarations...), canonical.ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func Tools(request canonical.CanonicalRequest) []canonical.ToolDeclaration {
	environment, err := canonical.ToolEnvironmentAt(request.Items(), len(request.Items()))
	if err != nil {
		return nil
	}
	return environment.Declarations()
}

func HasRequestToolDeclarations(request canonical.CanonicalRequest) bool {
	prelude, _, err := canonical.SplitRequestPrelude(request.Items())
	if err != nil {
		return false
	}
	for _, item := range prelude.Items() {
		if _, ok := item.ToolDeclarations(); ok {
			return true
		}
	}
	return false
}

func MustInstruction(role canonical.MessageRole, text string) canonical.CanonicalItem {
	item, err := canonical.NewScopedMessageItem(role, []canonical.MessagePart{canonical.NewTextMessagePart(text)}, canonical.ContextScopeRequest)
	if err != nil {
		panic(err)
	}
	return item
}

func DirectiveText(items []canonical.CanonicalItem) string {
	var out strings.Builder
	count := 0
	for _, item := range items {
		message, ok := item.Message()
		if !ok || (message.Role() != canonical.MessageRoleSystem && message.Role() != canonical.MessageRoleDeveloper) {
			continue
		}
		if count > 0 {
			out.WriteString("\n\n")
		}
		for _, part := range message.Content() {
			if text, ok := part.Text(); ok {
				out.WriteString(text.Text())
			}
		}
		count++
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

// LargeIntegerRequest exercises every raw JSON-bearing request surface through
// provider dialect mutation without allowing float64 reinterpretation.
func LargeIntegerRequest(t testing.TB, model string) canonical.CanonicalRequest {
	t.Helper()
	const large = "9007199254740993"
	key := MustRequestToolKey(canonical.ToolKindFunction, "probe")
	tool := MustFunctionTool(key, "probe", Schema(t, `{"type":"object","properties":{"value":{"enum":[`+large+`]}}}`), canonical.Unspecified[bool]())
	call := ToolCall(t, "call_probe", key, canonical.NewJSONObjectToolInput(Object(t, `{"value":`+large+`}`)))
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind: canonical.OutputFormatJSONSchema, Name: "probe_result",
		Schema: canonical.NewRawJSONObject(`{"type":"object","properties":{"value":{"enum":[` + large + `]}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        canonical.Specify(model),
		Items:        []canonical.CanonicalItem{ToolDeclarations(t, tool), Message(t, canonical.MessageRoleUser, "probe"), call},
		OutputFormat: canonical.Specify(format),
	})
}
