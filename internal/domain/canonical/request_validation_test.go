package canonical

import "testing"

func TestValidateMaterializedRequestRejectsEmptyResidualOperation(t *testing.T) {
	if err := ValidateMaterializedRequest(NewCanonicalRequest(RequestParams{})); err == nil {
		t.Fatal("ValidateMaterializedRequest accepted an empty residual request")
	}
}

func TestValidateMaterializedRequestRejectsRequiredPolicyAfterAllToolsErase(t *testing.T) {
	request := NewCanonicalRequest(RequestParams{
		Items:      []CanonicalItem{mustValidationMessage(t, "run")},
		ToolPolicy: Specify(NewToolPolicy(ToolPolicyRequired, nil)),
	})
	if err := ValidateMaterializedRequest(request); err == nil {
		t.Fatal("ValidateMaterializedRequest accepted required policy without surviving tools")
	}
}

func TestValidateMaterializedRequestRejectsDuplicateResults(t *testing.T) {
	callID, _ := NewToolCallID("call_1")
	key, _ := NewRequestToolKey(ToolKindFunction, "lookup")
	call, _ := NewToolCallItem(callID, key, NewJSONObjectToolInput(mustValidationObject(t, `{}`)))
	result, err := NewToolResultItem(callID, []ToolResultPart{NewTextToolResultPart("done")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateMaterializedRequest(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{call, result, result}})); err == nil {
		t.Fatal("ValidateMaterializedRequest accepted duplicate results")
	}
}

func TestValidateMaterializedRequestRejectsFunctionCallWithWebSearchResult(t *testing.T) {
	callID, _ := NewToolCallID("call_1")
	key, _ := NewRequestToolKey(ToolKindFunction, "lookup")
	call, _ := NewToolCallItem(callID, key, NewJSONObjectToolInput(mustValidationObject(t, `{}`)))
	result, _ := NewWebSearchResult(nil)
	resultItem, _ := NewWebSearchResultItem(callID, result)
	if err := ValidateMaterializedRequest(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{call, resultItem}})); err == nil {
		t.Fatal("ValidateMaterializedRequest accepted a web-search result for a function call")
	}
}

func TestValidateMaterializedRequestRejectsCustomCallWithWebSearchResult(t *testing.T) {
	callID, _ := NewToolCallID("call_1")
	key, _ := NewRequestToolKey(ToolKindCustom, "shell")
	call, _ := NewToolCallItem(callID, key, NewTextToolInput("run"))
	result, _ := NewWebSearchResult(nil)
	resultItem, _ := NewWebSearchResultItem(callID, result)
	if err := ValidateMaterializedRequest(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{call, resultItem}})); err == nil {
		t.Fatal("ValidateMaterializedRequest accepted a web-search result for a custom call")
	}
}

func TestValidateMaterializedRequestRejectsWebSearchCallWithContentResult(t *testing.T) {
	callID, _ := NewToolCallID("search_1")
	input, _ := NewWebSearchToolInput(WebSearchCall{Action: WebSearchActionSearch})
	call, _ := NewToolCallItem(callID, WebSearchToolKey(), input)
	result, _ := NewToolResultItem(callID, []ToolResultPart{NewTextToolResultPart("done")}, false)
	if err := ValidateMaterializedRequest(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{call, result}})); err == nil {
		t.Fatal("ValidateMaterializedRequest accepted a content result for a web-search call")
	}
}

func TestValidateMaterializedRequestAcceptsWebSearchCallWithWebSearchResult(t *testing.T) {
	callID, _ := NewToolCallID("search_1")
	input, _ := NewWebSearchToolInput(WebSearchCall{Action: WebSearchActionSearch})
	call, _ := NewToolCallItem(callID, WebSearchToolKey(), input)
	result, _ := NewWebSearchResult(nil)
	resultItem, _ := NewWebSearchResultItem(callID, result)
	if err := ValidateMaterializedRequest(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{call, resultItem}})); err != nil {
		t.Fatalf("ValidateMaterializedRequest rejected a matching web-search lifecycle: %v", err)
	}
}

func TestValidateMaterializedRequestAllowsCallIDReuseAfterCompletedPair(t *testing.T) {
	callID, _ := NewToolCallID("call_1")
	key, _ := NewRequestToolKey(ToolKindFunction, "lookup")
	call, _ := NewToolCallItem(callID, key, NewJSONObjectToolInput(mustValidationObject(t, `{}`)))
	result, _ := NewToolResultItem(callID, []ToolResultPart{NewTextToolResultPart("done")}, false)
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{call, result, call, result}})
	if err := ValidateMaterializedRequest(request); err != nil {
		t.Fatalf("ValidateMaterializedRequest rejected unambiguous call ID reuse: %v", err)
	}
}

func TestValidateMaterializedRequestAcceptsPendingCallAsMeaningfulHistory(t *testing.T) {
	callID, _ := NewToolCallID("call_1")
	key, _ := NewRequestToolKey(ToolKindFunction, "lookup")
	call, _ := NewToolCallItem(callID, key, NewJSONObjectToolInput(mustValidationObject(t, `{}`)))
	if err := ValidateMaterializedRequest(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{call}})); err != nil {
		t.Fatalf("ValidateMaterializedRequest rejected a pending historical call: %v", err)
	}
}

func TestValidateMaterializedRequestAcceptsCompletedHistoricalCallWithoutCurrentDeclaration(t *testing.T) {
	callID, _ := NewToolCallID("call_1")
	key, _ := NewRequestToolKey(ToolKindFunction, "lookup")
	call, _ := NewToolCallItem(callID, key, NewJSONObjectToolInput(mustValidationObject(t, `{}`)))
	result, _ := NewToolResultItem(callID, []ToolResultPart{NewTextToolResultPart("done")}, false)
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{call, result, mustValidationMessage(t, "continue")}})
	if err := ValidateMaterializedRequest(request); err != nil {
		t.Fatalf("ValidateMaterializedRequest required a current declaration for historical call truth: %v", err)
	}
}

func TestValidateMaterializedRequestAcceptsReasoningOnlyHistory(t *testing.T) {
	part, _ := NewReasoningPart(ReasoningPartSummary, "considering")
	reasoning, _ := NewReasoningItem([]ReasoningPart{part}, OpaqueThinking{})
	if err := ValidateMaterializedRequest(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{reasoning}})); err != nil {
		t.Fatalf("ValidateMaterializedRequest rejected reasoning-only history: %v", err)
	}
}

func TestValidateMaterializedRequestRejectsDiscoveryExecutorMismatch(t *testing.T) {
	callID, _ := NewToolCallID("search_1")
	call, _ := NewToolDiscoveryCallItem(callID, NewJSONObjectToolInput(mustValidationObject(t, `{}`)), DiscoveryExecutorClient)
	result, _ := NewToolDiscoveryResultItem(callID, ToolSet{}, DiscoveryExecutorProvider)
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{call, result}})
	if err := ValidateMaterializedRequest(request); err == nil {
		t.Fatal("ValidateMaterializedRequest accepted a discovery result with a different executor")
	}
}

func TestValidateMaterializedRequestAcceptsRequestScopedDirectiveOnly(t *testing.T) {
	directive := mustValidationScopedMessage(t, MessageRoleDeveloper, "follow policy")
	if err := ValidateMaterializedRequest(NewCanonicalRequest(RequestParams{Items: []CanonicalItem{directive}})); err != nil {
		t.Fatalf("ValidateMaterializedRequest returned error: %v", err)
	}
}

func TestValidateMaterializedRequestAcceptsRequestScopedDirectiveWithTools(t *testing.T) {
	directive := mustValidationScopedMessage(t, MessageRoleSystem, "use search")
	key, _ := NewRequestToolKey(ToolKindFunction, "search")
	schemaObject, _ := ParseJSONObject([]byte(`{"type":"object"}`))
	declaration, _ := NewFunctionTool(key, "", NewToolSchemaObject(schemaObject), Unspecified[bool]())
	set, _ := NewToolSet([]ToolDeclaration{declaration})
	tools, _ := NewToolDeclarationsItem(set, ContextScopeRequest)
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{tools, directive}})
	if err := ValidateMaterializedRequest(request); err != nil {
		t.Fatalf("ValidateMaterializedRequest returned error: %v", err)
	}
}

func TestValidateMaterializedRequestAcceptsRequestScopedDirectiveWithPreviousResponse(t *testing.T) {
	directive := mustValidationScopedMessage(t, MessageRoleDeveloper, "continue carefully")
	previous := ResponseRef{SwobuID: NewSwobuResponseID("resp_1")}
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{directive}, PreviousResponse: &previous})
	if err := ValidateMaterializedRequest(request); err != nil {
		t.Fatalf("ValidateMaterializedRequest returned error: %v", err)
	}
}

func mustValidationMessage(t *testing.T, text string) CanonicalItem {
	t.Helper()
	item, err := NewMessageItem(MessageRoleUser, []MessagePart{NewTextMessagePart(text)})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func mustValidationScopedMessage(t *testing.T, role MessageRole, text string) CanonicalItem {
	t.Helper()
	item, err := NewScopedMessageItem(role, []MessagePart{NewTextMessagePart(text)}, ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func mustValidationObject(t *testing.T, raw string) JSONObject {
	t.Helper()
	object, err := ParseJSONObject([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return object
}
