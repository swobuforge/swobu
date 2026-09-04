package continuity

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

const testWorkspaceSlug = "test-ns"

func testBackendTarget(t *testing.T, model string) provider.TargetSnapshot {
	t.Helper()
	target := provider.NewTargetSnapshot("target-"+model, "openai", "https://api.openai.com", "test", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	target.Model = model
	return target
}

func makeRequest(model string, items []canonical.CanonicalItem, previous *canonical.ResponseRef) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(model), Items: items, PreviousResponse: previous,
		Controls: canonical.GenerationControls{Limits: canonical.GenerationLimits{MaxOutputTokens: canonical.NewOptionalInt(100)}},
	})
}

func makeItems(texts ...string) []canonical.CanonicalItem {
	items := make([]canonical.CanonicalItem, 0, len(texts))
	for _, text := range texts {
		items = append(items, mustMessageItem(canonical.MessageRoleUser, text))
	}
	return items
}

func mustMessageItem(author canonical.MessageRole, text string) canonical.CanonicalItem {
	item, err := canonical.NewMessageItem(author, []canonical.MessagePart{canonical.NewTextMessagePart(text)})
	if err != nil {
		panic(err)
	}
	return item
}

func makeResponse(items ...canonical.CanonicalItem) canonical.CanonicalResponse {
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: "resp_output"}, "gpt-4o", items, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		panic(err)
	}
	return response
}

func checkpoint(id canonical.SwobuResponseID, request canonical.CanonicalRequest, response canonical.CanonicalResponse, responses *canonical.ResponsesContinuation) Checkpoint {
	bound, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id, Responses: responses}, response.Model(), response.Items(), response.Completion(), response.Usage())
	if err != nil {
		panic(err)
	}
	return Checkpoint{Request: request, Response: bound}
}

func interactionsCheckpoint(id canonical.SwobuResponseID, request canonical.CanonicalRequest, response canonical.CanonicalResponse, interactions *canonical.InteractionsContinuation) Checkpoint {
	bound, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id, Interactions: interactions}, response.Model(), response.Items(), response.Completion(), response.Usage())
	if err != nil {
		panic(err)
	}
	return Checkpoint{Request: request, Response: bound}
}

func nativeResponses(target provider.TargetSnapshot, providerResponseID string) *canonical.ResponsesContinuation {
	return &canonical.ResponsesContinuation{ProviderResponseID: canonical.NewResponsesResponseID(providerResponseID), TargetID: target.TargetID, TargetVersion: target.TargetVersion}
}

func nativeInteractions(target provider.TargetSnapshot, providerInteractionID string) *canonical.InteractionsContinuation {
	return &canonical.InteractionsContinuation{ProviderInteractionID: canonical.NewInteractionID(providerInteractionID), TargetID: target.TargetID, TargetVersion: target.TargetVersion}
}

func mustTestToolSet(t *testing.T, declarations ...canonical.ToolDeclaration) canonical.ToolSet {
	t.Helper()
	set, err := canonical.NewToolSet(declarations)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestBeginRejectsPreviousResponse(t *testing.T) {
	request := makeRequest("gpt-4o", makeItems("hello"), &canonical.ResponseRef{SwobuID: "resp_old"})
	if _, err := Begin(request); err == nil || err.Error() != "thread begin request contains previous response" {
		t.Fatalf("Begin error = %v", err)
	}
}

func TestResumeDoesNotInheritReasoningControlsAndPreservesOpaqueThinking(t *testing.T) {
	previousReasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: canonical.Specify(canonical.NewAutomaticReasoningCompute()), Disclosure: canonical.Specify(canonical.ReasoningDisclosureSummary),
		ResponsesContext: canonical.Specify(canonical.ResponsesReasoningContextAllTurns),
	})
	if err != nil {
		t.Fatal(err)
	}
	effort := canonical.InferenceEffortHigh
	previousControls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	previousRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: makeItems("one"), Controls: previousControls, Reasoning: previousReasoning,
	})
	opaque, err := canonical.NewMessagesOpaqueThinking([]byte(`{"type":"thinking","thinking":"summary","signature":"durable-signature"}`))
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "summary")
	if err != nil {
		t.Fatal(err)
	}
	reasoningItem, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	record := checkpoint("resp_previous", previousRequest, makeResponse(reasoningItem), nil)
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: makeItems("two"), PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
	})

	resolved, err := Resume(current, record)
	if err != nil {
		t.Fatal(err)
	}
	request := resolved.Request()
	if _, ok := request.Reasoning().ComputeField().Get(); ok {
		t.Fatal("resolved request inherited omitted reasoning compute")
	}
	if _, ok := request.Controls().Effort.Get(); ok {
		t.Fatal("resolved request inherited omitted inference effort")
	}
	if request.Reasoning().ResponsesContextField().IsSpecified() {
		t.Fatal("resolved request inherited omitted Responses reasoning context")
	}
	items := resolved.Request().Items()
	if len(items) != 3 || items[1].Kind() != canonical.ItemKindReasoning {
		t.Fatalf("materialized reasoning items = %#v", items)
	}
	restored, _ := items[1].Reasoning()
	messages, ok := restored.Opaque().Messages()
	if !ok {
		t.Fatal("materialization lost Messages opaque thinking")
	}
	if !ok || !strings.Contains(string(messages), "durable-signature") {
		t.Fatal("materialization lost exact signature bytes")
	}
}

func TestResumeKeepsCurrentReasoningControlsForMatchingToolResults(t *testing.T) {
	previousReasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(canonical.NewAutomaticReasoningCompute())})
	if err != nil {
		t.Fatal(err)
	}
	effort := canonical.InferenceEffortHigh
	previousControls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	previousRequest := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: makeItems("one"), Controls: previousControls, Reasoning: previousReasoning})
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	opaque, err := canonical.NewMessagesOpaqueThinking([]byte(`{"type":"thinking","signature":"tool-turn-signature"}`))
	if err != nil {
		t.Fatal(err)
	}
	reasoningPart, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "tool reasoning")
	if err != nil {
		t.Fatal(err)
	}
	reasoningItem, err := canonical.NewReasoningItem([]canonical.ReasoningPart{reasoningPart}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	record := checkpoint("resp_previous", previousRequest, makeResponse(reasoningItem, call), nil)
	callID, _ := canonical.NewToolCallID("call_1")
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	if err != nil {
		t.Fatal(err)
	}
	currentCompute, err := canonical.NewBudgetReasoningCompute(2048)
	if err != nil {
		t.Fatal(err)
	}
	currentEffort := canonical.InferenceEffortLow
	currentControls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &currentEffort})
	if err != nil {
		t.Fatal(err)
	}
	currentReasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(currentCompute)})
	if err != nil {
		t.Fatal(err)
	}
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
		Controls:         currentControls, Reasoning: currentReasoning,
	})

	resolved, err := Resume(current, record)
	if err != nil {
		t.Fatal(err)
	}
	request := resolved.Request()
	compute, computeSet := request.Reasoning().ComputeField().Get()
	gotEffort, effortSet := request.Controls().Effort.Get()
	if !computeSet || compute != currentCompute || !effortSet || gotEffort != currentEffort {
		t.Fatal("resolved request did not retain current request controls")
	}
	items := request.Items()
	if len(items) < 3 || items[1].Kind() != canonical.ItemKindReasoning {
		t.Fatalf("resolved history = %#v, want preserved opaque reasoning", items)
	}
	restoredReasoning, _ := items[1].Reasoning()
	restoredOpaque, ok := restoredReasoning.Opaque().Messages()
	if !ok || !strings.Contains(string(restoredOpaque), "tool-turn-signature") {
		t.Fatal("tool continuation lost opaque reasoning state")
	}

	omitted := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
	})
	resolvedOmitted, err := Resume(omitted, record)
	if err != nil {
		t.Fatal(err)
	}
	if _, set := resolvedOmitted.Request().Reasoning().ComputeField().Get(); set {
		t.Fatal("tool continuation inherited checkpoint reasoning compute")
	}
	if _, set := resolvedOmitted.Request().Controls().Effort.Get(); set {
		t.Fatal("tool continuation inherited checkpoint inference effort")
	}

	unrelatedID, _ := canonical.NewToolCallID("call_other")
	unrelated, _ := canonical.NewToolResultItem(unrelatedID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	unrelatedRequest := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{unrelated}, PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"}})
	if _, err := Resume(unrelatedRequest, record); err == nil {
		t.Fatal("foreign tool result silently abandoned unfinished turn")
	}
	secondCall := canonicaltest.ToolCall(t, "call_2", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	multipleRecord := checkpoint("resp_previous", previousRequest, makeResponse(call, secondCall), nil)
	if _, err := Resume(current, multipleRecord); err != nil {
		t.Fatalf("partial valid tool results were rejected: %v", err)
	}
	duplicate := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result, result}, PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
	})
	if _, err := Resume(duplicate, multipleRecord); err == nil {
		t.Fatal("duplicate tool results silently abandoned unfinished turn")
	}
}

func TestResumeValidatesRequiredPolicyAfterRestoringUnfinishedTurnTools(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search")
	tool := canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	previous := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      canonical.Specify("m"),
		Items:      []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, tool), canonicaltest.Message(t, canonical.MessageRoleUser, "search")},
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)),
	})
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	record := checkpoint("resp_previous", previous, makeResponse(call), nil)
	callID, _ := canonical.NewToolCallID("call_1")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify("m"),
		Items:            []canonical.CanonicalItem{result},
		ToolPolicy:       canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)),
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
	})

	resolved, err := Resume(current, record)
	if err != nil {
		t.Fatalf("Resume rejected valid restored tool environment: %v", err)
	}
	if len(canonicaltest.Tools(resolved.Request())) != 1 {
		t.Fatalf("resolved tools = %#v, want restored search declaration", canonicaltest.Tools(resolved.Request()))
	}
}

func TestBeginValidatesRequiredPolicyAfterSessionMaterialization(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      canonical.Specify("m"),
		Items:      makeItems("run"),
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)),
	})
	if _, err := Begin(request); err == nil {
		t.Fatal("Begin accepted required policy without a materialized tool")
	}
}

func TestBeginRejectsOrphanToolResult(t *testing.T) {
	callID, _ := canonical.NewToolCallID("call_1")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	request := makeRequest("m", []canonical.CanonicalItem{result}, nil)
	if _, err := Begin(request); err == nil {
		t.Fatal("Begin accepted a tool result without a preceding call")
	}
}

func TestBeginRejectsOrphanDiscoveryResult(t *testing.T) {
	callID, _ := canonical.NewToolCallID("search_1")
	loaded := mustTestToolSet(t, canonical.NewWebSearchDeclaration())
	result, _ := canonical.NewToolDiscoveryResultItem(callID, loaded, canonical.DiscoveryExecutorClient)
	request := makeRequest("m", []canonical.CanonicalItem{result}, nil)
	if _, err := Begin(request); err == nil {
		t.Fatal("Begin accepted a discovery result without a preceding discovery call")
	}
}

func TestResumeRepeatsRequestContextOnlyForMatchingUnfinishedTurn(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	tool := canonicaltest.MustFunctionTool(key, "lookup", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	previousRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{
			canonicaltest.MustInstruction(canonical.MessageRoleDeveloper, "base"),
			canonicaltest.ToolDeclarations(t, tool),
			canonicaltest.Message(t, canonical.MessageRoleUser, "start"),
		},
	})
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	target := testBackendTarget(t, "m")
	record := checkpoint("resp_previous", previousRequest, makeResponse(call), nativeResponses(target, "provider_previous"))
	callID, _ := canonical.NewToolCallID("call_1")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
	})
	resolved, err := Resume(current, record)
	if err != nil {
		t.Fatal(err)
	}
	prelude, rest, err := canonical.SplitRequestPrelude(resolved.Request().Items())
	if err != nil {
		t.Fatal(err)
	}
	if len(prelude.Items()) != 2 || len(rest) != 3 || rest[2].Kind() != canonical.ItemKindToolResult {
		t.Fatalf("unfinished-turn complete request = %#v", resolved.Request().Items())
	}
	if previous, ok := resolved.PreviousHistory(target.TargetID, target.TargetVersion); !ok || previous.OmitStart != 2 || previous.OmitEnd != 4 || previous.Response.Responses == nil {
		t.Fatalf("unfinished-turn previous history = (%#v, %t)", previous, ok)
	}

	completedRecord := checkpoint("resp_previous", previousRequest, makeResponse(
		mustMessageItem(canonical.MessageRoleAssistant, "done"),
	), nil)
	next := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: makeItems("next"),
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
	})
	completed, err := Resume(next, completedRecord)
	if err != nil {
		t.Fatal(err)
	}
	prelude, _, err = canonical.SplitRequestPrelude(completed.Request().Items())
	if err != nil {
		t.Fatal(err)
	}
	if len(prelude.Items()) != 0 {
		t.Fatalf("completed turn retained request context: %#v", prelude.Items())
	}
}

func TestResumeReplacesCurrentRequestContextBandsAtomically(t *testing.T) {
	oldKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "old")
	newKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "new")
	oldTool := canonicaltest.MustFunctionTool(oldKey, "old", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	newTool := canonicaltest.MustFunctionTool(newKey, "new", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	previous := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{
			canonicaltest.MustInstruction(canonical.MessageRoleDeveloper, "old instructions"),
			canonicaltest.ToolDeclarations(t, oldTool),
			canonicaltest.Message(t, canonical.MessageRoleUser, "start"),
		},
	})
	call := canonicaltest.ToolCall(t, "call_1", oldKey, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	record := checkpoint("resp_previous", previous, makeResponse(call), nil)
	callID, _ := canonical.NewToolCallID("call_1")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	currentDirective, _ := canonical.NewScopedMessageItem(
		canonical.MessageRoleDeveloper,
		[]canonical.MessagePart{canonical.NewTextMessagePart("new instructions")},
		canonical.ContextScopeRequest,
	)
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{
			currentDirective,
			canonicaltest.ToolDeclarations(t, newTool),
			result,
		},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
	})
	resolved, err := Resume(current, record)
	if err != nil {
		t.Fatal(err)
	}
	prelude, _, err := canonical.SplitRequestPrelude(resolved.Request().Items())
	if err != nil {
		t.Fatal(err)
	}
	preludeItems := prelude.Items()
	if len(preludeItems) != 2 {
		t.Fatalf("resolved prelude = %#v", preludeItems)
	}
	message, _ := preludeItems[0].Message()
	text, _ := message.Content()[0].Text()
	if text.Text() != "new instructions" {
		t.Fatalf("resolved directive = %q", text.Text())
	}
	declarations, _ := preludeItems[1].ToolDeclarations()
	if _, found := declarations.Tools().Lookup(oldKey); found {
		t.Fatal("previous tool band was unioned into explicit current tools")
	}
	if _, found := declarations.Tools().Lookup(newKey); !found {
		t.Fatal("current tool band was lost")
	}
}

func TestResumeExplicitEmptyDirectiveDoesNotRepeatPriorDirective(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	tool := canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	previous := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{
			canonicaltest.MustInstruction(canonical.MessageRoleSystem, "old instructions"),
			canonicaltest.ToolDeclarations(t, tool),
			canonicaltest.Message(t, canonical.MessageRoleUser, "start"),
		},
	})
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	record := checkpoint("resp_previous", previous, makeResponse(call), nil)
	callID, _ := canonical.NewToolCallID("call_1")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	clear, _ := canonical.NewScopedMessageItem(
		canonical.MessageRoleSystem,
		[]canonical.MessagePart{canonical.NewTextMessagePart("")},
		canonical.ContextScopeRequest,
	)
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{clear, result},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
	})
	resolved, err := Resume(current, record)
	if err != nil {
		t.Fatal(err)
	}
	prelude, _, err := canonical.SplitRequestPrelude(resolved.Request().Items())
	if err != nil {
		t.Fatal(err)
	}
	preludeItems := prelude.Items()
	if len(preludeItems) != 2 {
		t.Fatalf("resolved prelude = %#v", preludeItems)
	}
	message, _ := preludeItems[0].Message()
	text, _ := message.Content()[0].Text()
	if text.Text() != "" {
		t.Fatalf("explicit empty directive inherited %q", text.Text())
	}
}

func TestSplitRequestContextBandsRejectsAlternatingBands(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	tool := canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	items := []canonical.CanonicalItem{
		canonicaltest.MustInstruction(canonical.MessageRoleSystem, "a"),
		canonicaltest.ToolDeclarations(t, tool),
		canonicaltest.MustInstruction(canonical.MessageRoleDeveloper, "b"),
	}
	if _, _, err := canonical.SplitRequestPrelude(items); err == nil {
		t.Fatal("alternating context bands were reordered instead of rejected")
	}
}

func TestResumeRejectsToolResultWhenCheckpointHasNoToolCall(t *testing.T) {
	previousRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: makeItems("one"),
	})
	record := checkpoint("resp_previous", previousRequest, makeResponse(
		mustMessageItem(canonical.MessageRoleAssistant, "done"),
	), nil)
	foreignID, err := canonical.NewToolCallID("call_foreign")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := canonical.NewToolResultItem(foreignID, []canonical.ToolResultPart{
		canonical.NewTextToolResultPart("unexpected"),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{foreign},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
	})

	if _, err := Resume(current, record); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("Resume error = %v, want foreign tool-result rejection", err)
	}
}

func TestResumeAcceptsPartialResultsForInterleavedToolCalls(t *testing.T) {
	previousRequest := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: makeItems("one")})
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	callOne := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	callTwo := canonicaltest.ToolCall(t, "call_2", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	partA, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "A")
	partB, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "B")
	reasoningA, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{partA}, canonical.OpaqueThinking{})
	reasoningB, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{partB}, canonical.OpaqueThinking{})
	record := checkpoint("resp_previous", previousRequest, makeResponse(reasoningA, callOne, reasoningB, callTwo), nil)
	callOneID, _ := canonical.NewToolCallID("call_1")
	callTwoID, _ := canonical.NewToolCallID("call_2")
	resultOne, _ := canonical.NewToolResultItem(callOneID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("one")}, false)
	resultTwo, _ := canonical.NewToolResultItem(callTwoID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("two")}, false)
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify("m"),
		Items:            []canonical.CanonicalItem{resultOne},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
	})

	resolved, err := Resume(current, record)
	if err != nil {
		t.Fatalf("interleaved prior response did not resume: %v", err)
	}
	if len(resolved.Request().Items()) != 6 {
		t.Fatalf("materialized items = %d, want checkpoint request + full interleaved response + partial result", len(resolved.Request().Items()))
	}
	_ = resultTwo
	_ = callTwoID
}

func TestResumeExplicitDisabledReasoningClearsInheritedDisclosure(t *testing.T) {
	previousReasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute:    canonical.Specify(canonical.NewAutomaticReasoningCompute()),
		Disclosure: canonical.Specify(canonical.ReasoningDisclosureSummary),
	})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Compute: canonical.Specify(canonical.NewDisabledReasoningCompute()),
	})
	if err != nil {
		t.Fatal(err)
	}
	previousRequest := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: makeItems("one"), Reasoning: previousReasoning})
	record := checkpoint("resp_previous", previousRequest, makeResponse(mustMessageItem(canonical.MessageRoleAssistant, "answer")), nil)
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: makeItems("two"), PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"}, Reasoning: disabled,
	})
	resolved, err := Resume(current, record)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := resolved.Request().Reasoning().DisclosureField().Get(); present {
		t.Fatal("explicit disabled reasoning retained inherited disclosure")
	}
}

func TestBeginMaterializesEveryDefaultBearingBand(t *testing.T) {
	source := canonical.NewCanonicalRequest(canonical.RequestParams{Items: makeItems("turn")})
	prepared, err := Begin(source)
	if err != nil {
		t.Fatal(err)
	}
	semantic := prepared.Request()
	for name, specified := range map[string]bool{
		"model":              semantic.ModelSpecified(),
		"tool policy":        semantic.ToolPolicySpecified(),
		"tool-call batching": semantic.ToolCallBatchSpecified(),
		"output format":      semantic.OutputFormatSpecified(),
	} {
		if !specified {
			t.Fatalf("effective semantic request left %s omitted", name)
		}
	}
	explicit := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      canonical.Specify(""),
		Items:      makeItems("turn"),
		ToolPolicy: canonical.Specify(canonical.ToolPolicy{}), ToolCallBatch: canonical.Specify(canonical.ToolCallBatchPolicy{}), OutputFormat: canonical.Specify(canonical.OutputFormat{}),
	})
	explicitPrepared, err := Begin(explicit)
	if err != nil {
		t.Fatal(err)
	}
	explicitSemantic := explicitPrepared.Request()
	for name, specified := range map[string]bool{
		"model":              explicitSemantic.ModelSpecified(),
		"tool policy":        explicitSemantic.ToolPolicySpecified(),
		"tool-call batching": explicitSemantic.ToolCallBatchSpecified(),
		"output format":      explicitSemantic.OutputFormatSpecified(),
	} {
		if !specified {
			t.Fatalf("source delta lost explicit empty %s", name)
		}
	}
}

func TestResumeMaterializesOrderedHistory(t *testing.T) {
	previous := makeRequest("gpt-4o", makeItems("turn1"), nil)
	record := checkpoint("resp_prev", previous, makeResponse(mustMessageItem(canonical.MessageRoleAssistant, "assistant1")), nil)
	prepared, err := Resume(makeRequest("gpt-4o", makeItems("turn2"), &canonical.ResponseRef{SwobuID: "resp_prev"}), record)
	if err != nil {
		t.Fatal(err)
	}
	items := prepared.Request().Items()
	if len(items) != 3 || canonicalItemText(items[0]) != "turn1" || canonicalItemText(items[1]) != "assistant1" || canonicalItemText(items[2]) != "turn2" {
		t.Fatalf("semantic history=%#v", items)
	}
}

func TestResumeUsesFieldLocalPresenceForExplicitClears(t *testing.T) {
	tool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search"), "", canonical.NewToolSchemaObject(canonicaltest.Object(t, `{"type":"object"}`)), canonical.Unspecified[bool]())
	structured, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONSchema, Name: "answer", Schema: canonical.NewRawJSONObject(`{"type":"object"}`)})
	if err != nil {
		t.Fatal(err)
	}
	text, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatText})
	if err != nil {
		t.Fatal(err)
	}
	previous := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("old"), Items: []canonical.CanonicalItem{
			canonicaltest.MustInstruction(canonical.MessageRoleSystem, "concise"),
			canonicaltest.ToolDeclarations(t, tool),
		},
		ToolPolicy:    canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)),
		ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)), OutputFormat: canonical.Specify(structured),
		Controls: canonical.GenerationControls{Limits: canonical.GenerationLimits{StopSequences: []string{"END"}}},
	})
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify(""),
		Items:            makeItems("turn2"),
		ToolPolicy:       canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyNone, nil)),
		ToolCallBatch:    canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchUnspecified)),
		OutputFormat:     canonical.Specify(text),
		Controls:         canonical.GenerationControls{Limits: canonical.GenerationLimits{StopSequences: []string{}}},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_prev"},
	})
	prepared, err := Resume(current, checkpoint("resp_prev", previous, makeResponse(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Request().Model() != "" || canonicaltest.DirectiveText(prepared.Request().Items()) != "" || len(canonicaltest.Tools(prepared.Request())) != 0 || prepared.Request().Controls().Limits.StopSequences == nil || prepared.Request().ToolPolicy().Mode != canonical.ToolPolicyNone || prepared.Request().ToolCallBatch().Mode != canonical.ToolCallBatchUnspecified || prepared.Request().OutputFormat().Kind != canonical.OutputFormatText {
		t.Fatalf("explicit clears not retained: %#v", prepared.Request())
	}
}

func TestResumeDoesNotInheritCompletedTurnContextOrControls(t *testing.T) {
	previous := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      canonical.Specify("old"),
		Items:      []canonical.CanonicalItem{canonicaltest.MustInstruction(canonical.MessageRoleSystem, "concise")},
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)),
		Controls:   canonical.GenerationControls{Limits: canonical.GenerationLimits{StopSequences: []string{"END"}}},
	})
	current := canonical.NewCanonicalRequest(canonical.RequestParams{Items: makeItems("turn2"), PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_prev"}})
	prepared, err := Resume(current, checkpoint("resp_prev", previous, makeResponse(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Request().Model() != "old" || canonicaltest.DirectiveText(prepared.Request().Items()) != "" ||
		prepared.Request().ToolPolicySpecified() || prepared.Request().Controls().Limits.StopSequences != nil {
		t.Fatalf("completed-turn request state was retained: %#v", prepared.Request())
	}
}

func canonicalItemText(item canonical.CanonicalItem) string {
	message, ok := item.Message()
	if !ok {
		return ""
	}
	var out string
	for _, part := range message.Content() {
		if text, ok := part.Text(); ok {
			out += text.Text()
		}
	}
	return out
}
