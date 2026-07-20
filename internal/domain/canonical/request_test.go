package canonical

import "testing"

func TestCanonicalRequestOwnsSpecifiedBandsAndDeepClones(t *testing.T) {
	message, _ := NewMessageItem(MessageRoleUser, []MessagePart{NewTextMessagePart("hello")})
	decl := testFunctionTool(testRequestToolKey(ToolKindFunction, "lookup"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	tools, err := NewToolSet([]ToolDeclaration{decl})
	if err != nil {
		t.Fatal(err)
	}
	request := NewCanonicalRequest(RequestParams{
		Model: Specify("model"), Instructions: Specify(InstructionSet{}), Items: []CanonicalItem{message}, Tools: Specify(tools),
		ToolPolicy: Specify(NewToolPolicy(ToolPolicyAuto, nil)), OutputFormat: Specify(OutputFormat{}),
	})
	if !request.ModelSpecified() || !request.InstructionsSpecified() || !request.ToolsSpecified() {
		t.Fatal("specified request bands lost presence")
	}
	if !request.Instructions().IsEmpty() {
		t.Fatalf("explicit empty instructions = %#v", request.Instructions())
	}
	clone := request.Clone()
	if clone.Model() != "model" || len(clone.Items()) != 1 || len(clone.Tools()) != 1 {
		t.Fatal("clone lost request semantics")
	}
	items := clone.Items()
	replacement, _ := NewMessageItem(MessageRoleUser, []MessagePart{NewTextMessagePart("changed")})
	items[0] = replacement
	original, _ := request.Items()[0].Message()
	text, _ := original.Content()[0].Text()
	if text.Text() != "hello" {
		t.Fatal("request items aliased clone")
	}
}

func TestCanonicalRequestDistinguishesOmittedFromExplicitEmpty(t *testing.T) {
	omitted := NewCanonicalRequest(RequestParams{})
	explicit := NewCanonicalRequest(RequestParams{Instructions: Specify(InstructionSet{})})
	if omitted.InstructionsSpecified() || !explicit.InstructionsSpecified() {
		t.Fatal("field-local omission was not preserved")
	}
}
