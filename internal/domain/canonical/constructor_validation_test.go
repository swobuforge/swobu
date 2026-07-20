package canonical

import "testing"

func TestPublicConstructorsRejectInvalidStateImmediately(t *testing.T) {
	if _, err := NewRequestToolKey(ToolKindFunction, " padded "); err == nil {
		t.Fatal("request tool key normalized surrounding whitespace")
	}
	customKey, _ := NewToolKey(ToolNamespaceRequest, ToolKindCustom, "custom")
	if _, err := NewFunctionTool(customKey, "", EmptyToolSchema(), Unspecified[bool]()); err == nil {
		t.Fatal("function constructor returned a zero declaration")
	}
	functionKey, _ := NewToolKey(ToolNamespaceRequest, ToolKindFunction, "function")
	if _, err := NewCustomTool(functionKey, "", EmptyToolFormat()); err == nil {
		t.Fatal("custom constructor returned a zero declaration")
	}
	if _, err := NewMessageStart(MessageRole("invalid")); err == nil {
		t.Fatal("message start accepted an invalid role")
	}
	if _, err := NewToolCallStart(ToolCallID{}, functionKey); err == nil {
		t.Fatal("tool-call start returned a zero payload")
	}
	if _, err := NewToolCallID(" call_1 "); err == nil {
		t.Fatal("tool call ID normalized surrounding whitespace")
	}
}
