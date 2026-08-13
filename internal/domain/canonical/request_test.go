package canonical

import "testing"

func TestCanonicalRequestOwnsOrderedContextAndDeepClones(t *testing.T) {
	message, _ := NewMessageItem(MessageRoleUser, []MessagePart{NewTextMessagePart("hello")})
	decl := testFunctionTool(testRequestToolKey(ToolKindFunction, "lookup"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	tools, err := NewToolSet([]ToolDeclaration{decl})
	if err != nil {
		t.Fatal(err)
	}
	declarations, _ := NewToolDeclarationsItem(tools, ContextScopeRequest)
	request := NewCanonicalRequest(RequestParams{
		Model: Specify("model"), Items: []CanonicalItem{declarations, message},
		ToolPolicy: Specify(NewToolPolicy(ToolPolicyAuto, nil)), OutputFormat: Specify(OutputFormat{}),
	})
	if !request.ModelSpecified() {
		t.Fatal("specified model lost presence")
	}
	clone := request.Clone()
	environment, err := ToolEnvironmentAt(clone.Items(), len(clone.Items()))
	if err != nil || clone.Model() != "model" || len(clone.Items()) != 2 || len(environment.Declarations()) != 1 {
		t.Fatal("clone lost request semantics")
	}
	items := clone.Items()
	replacement, _ := NewMessageItem(MessageRoleUser, []MessagePart{NewTextMessagePart("changed")})
	items[1] = replacement
	original, _ := request.Items()[1].Message()
	text, _ := original.Content()[0].Text()
	if text.Text() != "hello" {
		t.Fatal("request items aliased clone")
	}
}

func TestCanonicalRequestStorePreservesPortablePersistenceIntent(t *testing.T) {
	for name, store := range map[string]Specified[bool]{
		"omitted": Unspecified[bool](),
		"false":   Specify(false),
		"true":    Specify(true),
	} {
		t.Run(name, func(t *testing.T) {
			request := NewCanonicalRequest(RequestParams{Model: Specify("model"), Store: store})
			wantValue, wantSpecified := store.Get()
			for _, derived := range []CanonicalRequest{request, request.Clone(), request.WithItems(nil)} {
				gotValue, gotSpecified := derived.Store()
				if gotValue != wantValue || gotSpecified != wantSpecified {
					t.Fatalf("store = (%t,%t), want (%t,%t)", gotValue, gotSpecified, wantValue, wantSpecified)
				}
				if gotEligible := derived.PersistenceEligible(); gotEligible != (!wantSpecified || wantValue) {
					t.Fatalf("persistence eligible = %t", gotEligible)
				}
			}
		})
	}
}
