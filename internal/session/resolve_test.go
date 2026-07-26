package session

import (
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

const testWorkspaceSlug = "test-ns"

func testBackendTarget(t *testing.T, model string) provider.TargetSnapshot {
	t.Helper()
	target := provider.NewTargetSnapshot("target-"+model, "openai", "https://api.openai.com", "test", "responses", "")
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
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: "resp_output"}, "gpt-4o", items, "stop", canonical.NewUnknownTokenUsage())
	if err != nil {
		panic(err)
	}
	return response
}

func checkpoint(id canonical.SwobuResponseID, request canonical.CanonicalRequest, response canonical.CanonicalResponse, responses *canonical.ResponsesContinuation) Checkpoint {
	bound, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id, Responses: responses}, response.Model(), response.Items(), response.CompletionReason(), response.Usage())
	if err != nil {
		panic(err)
	}
	return Checkpoint{Request: request, Response: bound}
}

func nativeResponses(target provider.TargetSnapshot, providerResponseID string) *canonical.ResponsesContinuation {
	return &canonical.ResponsesContinuation{ProviderResponseID: canonical.NewResponsesResponseID(providerResponseID), TargetID: target.TargetID, TargetVersion: target.TargetVersion}
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
	if _, err := Begin(request); err == nil || err.Error() != "session begin request contains previous response" {
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
	for name, request := range map[string]canonical.CanonicalRequest{"full": resolved.Full, "delta": resolved.Delta} {
		if _, ok := request.Reasoning().ComputeField().Get(); ok {
			t.Fatalf("%s inherited omitted reasoning compute", name)
		}
		if _, ok := request.Controls().Effort.Get(); ok {
			t.Fatalf("%s inherited omitted inference effort", name)
		}
		if request.Reasoning().ResponsesContextField().IsSpecified() {
			t.Fatalf("%s inherited omitted Responses reasoning context", name)
		}
	}
	items := resolved.Full.Items()
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

func TestResumeResolvesComputeOnlyForMatchingToolResults(t *testing.T) {
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
	record := checkpoint("resp_previous", previousRequest, makeResponse(call), nil)
	callID, _ := canonical.NewToolCallID("call_1")
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	if err != nil {
		t.Fatal(err)
	}
	current := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result}, PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"}})

	resolved, err := Resume(current, record)
	if err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]canonical.CanonicalRequest{"full": resolved.Full, "delta": resolved.Delta} {
		compute, computeSet := request.Reasoning().ComputeField().Get()
		gotEffort, effortSet := request.Controls().Effort.Get()
		if !computeSet || compute.Kind() != canonical.ReasoningAutomatic || !effortSet || gotEffort != effort {
			t.Fatalf("%s did not carry effective unfinished-turn compute", name)
		}
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
	record := checkpoint("resp_previous", previousRequest, makeResponse(call), nil)
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
	prelude, rest, err := canonical.SplitRequestPrelude(resolved.Delta.Items())
	if err != nil {
		t.Fatal(err)
	}
	if len(prelude.Items()) != 2 || len(rest) != 1 || rest[0].Kind() != canonical.ItemKindToolResult {
		t.Fatalf("unfinished-turn delta = %#v", resolved.Delta.Items())
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
	prelude, _, err = canonical.SplitRequestPrelude(completed.Delta.Items())
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
	prelude, _, err := canonical.SplitRequestPrelude(resolved.Delta.Items())
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
	prelude, _, err := canonical.SplitRequestPrelude(resolved.Delta.Items())
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
	if len(resolved.Full.Items()) != 6 {
		t.Fatalf("materialized items = %d, want checkpoint request + full interleaved response + partial result", len(resolved.Full.Items()))
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
	if _, present := resolved.Delta.Reasoning().DisclosureField().Get(); present {
		t.Fatal("explicit disabled reasoning retained inherited disclosure")
	}
}

func TestBeginMaterializesEveryDefaultBearingBand(t *testing.T) {
	source := canonical.NewCanonicalRequest(canonical.RequestParams{})
	prepared, err := Begin(source)
	if err != nil {
		t.Fatal(err)
	}
	semantic := prepared.Full
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
	for name, specified := range map[string]bool{
		"model":              prepared.Delta.ModelSpecified(),
		"tool policy":        prepared.Delta.ToolPolicySpecified(),
		"tool-call batching": prepared.Delta.ToolCallBatchSpecified(),
		"output format":      prepared.Delta.OutputFormatSpecified(),
	} {
		if specified {
			t.Fatalf("source delta changed omitted %s into explicit empty", name)
		}
	}
	explicit := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      canonical.Specify(""),
		ToolPolicy: canonical.Specify(canonical.ToolPolicy{}), ToolCallBatch: canonical.Specify(canonical.ToolCallBatchPolicy{}), OutputFormat: canonical.Specify(canonical.OutputFormat{}),
	})
	explicitPrepared, err := Begin(explicit)
	if err != nil {
		t.Fatal(err)
	}
	explicitDelta := explicitPrepared.Delta
	for name, specified := range map[string]bool{
		"model":              explicitDelta.ModelSpecified(),
		"tool policy":        explicitDelta.ToolPolicySpecified(),
		"tool-call batching": explicitDelta.ToolCallBatchSpecified(),
		"output format":      explicitDelta.OutputFormatSpecified(),
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
	items := prepared.Full.Items()
	if len(items) != 3 || canonicalItemText(items[0]) != "turn1" || canonicalItemText(items[1]) != "assistant1" || canonicalItemText(items[2]) != "turn2" {
		t.Fatalf("semantic history=%#v", items)
	}
}

func TestResumeUsesFieldLocalPresenceForExplicitClears(t *testing.T) {
	tool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search"), "", canonical.NewToolSchemaObject(mustJSONObject(t, `{"type":"object"}`)), canonical.Unspecified[bool]())
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
	if prepared.Full.Model() != "" || canonicaltest.DirectiveText(prepared.Full.Items()) != "" || len(canonicaltest.Tools(prepared.Full)) != 0 || prepared.Full.Controls().Limits.StopSequences == nil || prepared.Full.ToolPolicy().Mode != canonical.ToolPolicyNone || prepared.Full.ToolCallBatch().Mode != canonical.ToolCallBatchUnspecified || prepared.Full.OutputFormat().Kind != canonical.OutputFormatText {
		t.Fatalf("explicit clears not retained: %#v", prepared.Full)
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
	if prepared.Full.Model() != "old" || canonicaltest.DirectiveText(prepared.Full.Items()) != "" ||
		prepared.Full.ToolPolicySpecified() || prepared.Full.Controls().Limits.StopSequences != nil {
		t.Fatalf("completed-turn request state was retained: %#v", prepared.Full)
	}
}

func TestResolvedRequestUsesDeltaOnlyForApplicableNativeContinuation(t *testing.T) {
	target := testBackendTarget(t, "gpt-4o")
	prepared := ResolvedRequest{
		Full:  makeRequest("gpt-4o", makeItems("turn1", "assistant1", "turn2"), nil),
		Delta: makeRequest("gpt-4o", makeItems("turn2"), &canonical.ResponseRef{SwobuID: "resp_prev", Responses: nativeResponses(target, "provider_prev")}),
	}
	if got := prepared.ForTarget(target); len(got.Items()) != 1 {
		t.Fatalf("native continuation items=%d, want delta", len(got.Items()))
	}
	target.TargetVersion++
	if got := prepared.ForTarget(target); len(got.Items()) != 3 {
		t.Fatalf("changed target items=%d, want full history", len(got.Items()))
	}
}

func TestResumeIsDeterministicOverStoreResolvedCheckpoint(t *testing.T) {
	record := checkpoint("resp_prev", makeRequest("m", nil, nil), makeResponse(), nil)
	expired := time.Now().UTC().Add(-time.Minute)
	record.ExpiresAt = &expired
	if _, err := Resume(makeRequest("m", nil, &canonical.ResponseRef{SwobuID: "resp_prev"}), record); err != nil {
		t.Fatalf("store-resolved checkpoint rejected: %v", err)
	}
	if _, err := Resume(makeRequest("m", nil, &canonical.ResponseRef{SwobuID: "other"}), record); err == nil {
		t.Fatal("mismatched record accepted")
	}
}

func mustJSONObject(t *testing.T, raw string) canonical.JSONObject {
	t.Helper()
	object, err := canonical.ParseJSONObject([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return object
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
