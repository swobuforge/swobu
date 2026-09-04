package continuity

import (
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestResumeStoresCompleteRequestAndReturnsTargetGatedPreviousHistory(t *testing.T) {
	target := testBackendTarget(t, "m")
	previousRequest := makeRequest("m", makeItems("turn one"), nil)
	record := checkpoint("resp_previous", previousRequest, makeResponse(
		mustMessageItem(canonical.MessageRoleAssistant, "answer one"),
	), nativeResponses(target, "provider_previous"))
	resolved, err := Resume(makeRequest("m", makeItems("turn two"), &canonical.ResponseRef{SwobuID: "resp_previous"}), record)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(resolved.Request().Items()); got != 3 {
		t.Fatalf("complete request items = %d, want 3", got)
	}
	previous, ok := resolved.PreviousHistory(target.TargetID, target.TargetVersion)
	if !ok || previous.Response.Responses == nil || previous.Response.Responses.ProviderResponseID.String() != "provider_previous" || previous.OmitStart != 0 || previous.OmitEnd != 2 {
		t.Fatalf("PreviousHistory = (%#v, %t)", previous, ok)
	}
	if _, ok := resolved.PreviousHistory(target.TargetID+"-other", target.TargetVersion); ok {
		t.Fatal("target ID mismatch reused Responses continuation")
	}
	if _, ok := resolved.PreviousHistory(target.TargetID, target.TargetVersion+1); ok {
		t.Fatal("target version mismatch reused Responses continuation")
	}
}

func TestResumePreservesSettledWebSearchCanonicalTruth(t *testing.T) {
	target := testBackendTarget(t, "m")
	previousRequest := makeRequest("m", makeItems("find deadline"), nil)
	call, result, answer := settledWebSearchItems(t)
	record := checkpoint("resp_previous", previousRequest, makeResponse(call, result, answer), nativeResponses(target, "provider_previous"))
	resolved, err := Resume(makeRequest("m", makeItems("explain it"), &canonical.ResponseRef{SwobuID: "resp_previous"}), record)
	if err != nil {
		t.Fatal(err)
	}
	items := resolved.Request().Items()
	if len(items) != 5 || items[1].Kind() != canonical.ItemKindToolCall || items[2].Kind() != canonical.ItemKindToolResult || items[3].Kind() != canonical.ItemKindMessage {
		t.Fatalf("resumed items = %#v", items)
	}
	if got, want := items[1:4], []canonical.CanonicalItem{call, result, answer}; !reflect.DeepEqual(got, want) {
		t.Fatalf("resumed settled lifecycle changed:\n got  %#v\n want %#v", got, want)
	}
}

func TestWebSearchResumeContinuationIsExactTargetGated(t *testing.T) {
	target := testBackendTarget(t, "m")
	call, result, answer := settledWebSearchItems(t)
	record := checkpoint(
		"resp_previous", makeRequest("m", makeItems("find deadline"), nil),
		makeResponse(call, result, answer), nativeResponses(target, "provider_previous"),
	)
	resolved, err := Resume(makeRequest("m", makeItems("explain it"), &canonical.ResponseRef{SwobuID: "resp_previous"}), record)
	if err != nil {
		t.Fatal(err)
	}
	previous, ok := resolved.PreviousHistory(target.TargetID, target.TargetVersion)
	if !ok || previous.Response.Responses == nil || previous.Response.Responses.ProviderResponseID.String() != "provider_previous" {
		t.Fatalf("exact target continuation = (%#v, %t)", previous, ok)
	}
	if _, ok := resolved.PreviousHistory("local-vllm", target.TargetVersion); ok {
		t.Fatal("foreign target reused web-search native continuation")
	}
	if _, ok := resolved.PreviousHistory(target.TargetID, target.TargetVersion+1); ok {
		t.Fatal("changed target version reused web-search native continuation")
	}
}

func settledWebSearchItems(t *testing.T) (canonical.CanonicalItem, canonical.CanonicalItem, canonical.CanonicalItem) {
	t.Helper()
	callID, _ := canonical.NewToolCallID("search_1")
	input, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"deadline"}})
	if err != nil {
		t.Fatal(err)
	}
	refinement, err := canonical.NewResponsesWebSearchRefinement(canonical.ResponsesItemID("ws_provider_1"))
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItemWithResponsesWebSearch(callID, canonical.WebSearchToolKey(), input, &refinement)
	if err != nil {
		t.Fatal(err)
	}
	webURL, err := canonical.NewWebURL("https://example.test/rules")
	if err != nil {
		t.Fatal(err)
	}
	source, err := canonical.NewWebSource(webURL, canonical.Specify("Rules"))
	if err != nil {
		t.Fatal(err)
	}
	search, err := canonical.NewWebSearchResult([]canonical.WebSource{source})
	if err != nil {
		t.Fatal(err)
	}
	search, err = search.WithInteractionsReplay([]byte(`{"type":"google_search_result","id":"result-private"}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewWebSearchResultItem(callID, search)
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewCitedTextMessagePart("Hosted search answer", []canonical.WebCitation{{
		Source: source, Excerpt: canonical.Specify("Hosted"), Start: canonical.Specify(uint32(0)), End: canonical.Specify(uint32(6)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{part})
	if err != nil {
		t.Fatal(err)
	}
	return call, result, answer
}

func TestContinueAfterLocalResultMatchesCheckpointResumeSemantics(t *testing.T) {
	target := testBackendTarget(t, "m")
	base := makeRequest("m", makeItems("turn one"), nil)
	resolved, err := Begin(base)
	if err != nil {
		t.Fatal(err)
	}
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	callID, _ := canonical.NewToolCallID("call_equivalent")
	call := canonicaltest.ToolCall(t, callID.String(), key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	ref := canonical.ResponseRef{SwobuID: "resp_equivalent", Interactions: nativeInteractions(target, "interaction_equivalent")}
	response, err := canonical.NewCanonicalResponse(ref, "m", []canonical.CanonicalItem{call}, canonical.Completed("requires_action"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}

	local, err := resolved.ContinueAfterLocalResult(response, []canonical.CanonicalItem{result})
	if err != nil {
		t.Fatal(err)
	}
	scope := resolved.Request()
	external, err := Resume(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result}, PreviousResponse: &canonical.ResponseRef{SwobuID: ref.SwobuID},
		ToolPolicy: scope.ToolPolicyField(), ToolCallBatch: scope.ToolCallBatchField(), Controls: scope.Controls(),
		Reasoning: scope.Reasoning(), OutputFormat: scope.OutputFormatField(), Store: scope.StoreField(),
	}), Checkpoint{Request: base, Response: response})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(local.Request(), external.Request()) {
		t.Fatalf("local request = %#v, checkpoint/resume = %#v", local.Request(), external.Request())
	}
	localPrevious, localOK := local.PreviousHistory(target.TargetID, target.TargetVersion)
	externalPrevious, externalOK := external.PreviousHistory(target.TargetID, target.TargetVersion)
	if localOK != externalOK || !reflect.DeepEqual(localPrevious, externalPrevious) {
		t.Fatalf("local previous = (%#v,%t), checkpoint/resume = (%#v,%t)", localPrevious, localOK, externalPrevious, externalOK)
	}
}

func TestContinueAfterLocalResultValidatesFreshAuthorityAndFallbacks(t *testing.T) {
	target := testBackendTarget(t, "m")
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	callID, _ := canonical.NewToolCallID("call_fallback")
	call := canonicaltest.ToolCall(t, callID.String(), key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)

	for _, tc := range []struct {
		name      string
		store     canonical.Specified[bool]
		ref       canonical.ResponseRef
		targetID  string
		version   uint64
		wantFresh bool
	}{
		{name: "matching", ref: canonical.ResponseRef{SwobuID: "resp_matching", Interactions: nativeInteractions(target, "interaction_matching")}, targetID: target.TargetID, version: target.TargetVersion, wantFresh: true},
		{name: "no native handle", ref: canonical.ResponseRef{SwobuID: "resp_portable"}, targetID: target.TargetID, version: target.TargetVersion},
		{name: "store false", store: canonical.Specify(false), ref: canonical.ResponseRef{SwobuID: "resp_stateless", Interactions: nativeInteractions(target, "interaction_stateless")}, targetID: target.TargetID, version: target.TargetVersion},
		{name: "target changed", ref: canonical.ResponseRef{SwobuID: "resp_target", Interactions: nativeInteractions(target, "interaction_target")}, targetID: target.TargetID + "-other", version: target.TargetVersion},
		{name: "version changed", ref: canonical.ResponseRef{SwobuID: "resp_version", Interactions: nativeInteractions(target, "interaction_version")}, targetID: target.TargetID, version: target.TargetVersion + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := Begin(canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: makeItems("turn"), Store: tc.store}))
			if err != nil {
				t.Fatal(err)
			}
			response, err := canonical.NewCanonicalResponse(tc.ref, "m", []canonical.CanonicalItem{call}, canonical.Completed("requires_action"), canonical.NewUnknownTokenUsage())
			if err != nil {
				t.Fatal(err)
			}
			continued, err := resolved.ContinueAfterLocalResult(response, []canonical.CanonicalItem{result})
			if err != nil {
				t.Fatal(err)
			}
			previous, ok := continued.PreviousHistory(tc.targetID, tc.version)
			if ok != tc.wantFresh {
				t.Fatalf("PreviousHistory = (%#v,%t), want present=%t", previous, ok, tc.wantFresh)
			}
			if len(continued.Request().Items()) != len(resolved.Request().Items())+2 {
				t.Fatal("fallback changed complete canonical history")
			}
		})
	}

	resolved, _ := Begin(makeRequest("m", makeItems("turn"), nil))
	invalid, _ := canonical.NewCanonicalResponse(canonical.ResponseRef{}, "m", []canonical.CanonicalItem{call}, canonical.Completed("requires_action"), canonical.NewUnknownTokenUsage())
	if _, err := resolved.ContinueAfterLocalResult(invalid, []canonical.CanonicalItem{result}); err == nil {
		t.Fatal("local continuation accepted an uncommitted provider response")
	}
}

func TestContinueAfterLocalResultRotatesOnlyNewestAuthority(t *testing.T) {
	target := testBackendTarget(t, "m")
	resolved, _ := Begin(makeRequest("m", makeItems("turn"), nil))
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	for round := 1; round <= 2; round++ {
		callID, _ := canonical.NewToolCallID("call_" + string(rune('0'+round)))
		call := canonicaltest.ToolCall(t, callID.String(), key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{}`)))
		result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
		interaction := "interaction_" + string(rune('0'+round))
		ref := canonical.ResponseRef{SwobuID: canonical.SwobuResponseID("resp_" + string(rune('0'+round))), Interactions: nativeInteractions(target, interaction)}
		response, _ := canonical.NewCanonicalResponse(ref, "m", []canonical.CanonicalItem{call}, canonical.Completed("requires_action"), canonical.NewUnknownTokenUsage())
		var err error
		resolved, err = resolved.ContinueAfterLocalResult(response, []canonical.CanonicalItem{result})
		if err != nil {
			t.Fatal(err)
		}
		previous, ok := resolved.PreviousHistory(target.TargetID, target.TargetVersion)
		if !ok || previous.Response.Interactions.ProviderInteractionID.String() != interaction {
			t.Fatalf("round %d previous history = (%#v,%t)", round, previous, ok)
		}
		if previous.OmitEnd != uint32(len(resolved.Request().Items())-1) {
			t.Fatalf("round %d omit end = %d, want explicit newest result", round, previous.OmitEnd)
		}
	}
}

func TestResumeReturnsInteractionsHistoryOnlyForMatchingPersistentTarget(t *testing.T) {
	target := testBackendTarget(t, "m")
	previousRequest := makeRequest("m", makeItems("turn one"), nil)
	record := interactionsCheckpoint("resp_previous", previousRequest, makeResponse(
		mustMessageItem(canonical.MessageRoleAssistant, "answer one"),
	), nativeInteractions(target, "interaction_previous"))
	current := makeRequest("m", makeItems("turn two"), &canonical.ResponseRef{SwobuID: "resp_previous"})
	resolved, err := Resume(current, record)
	if err != nil {
		t.Fatal(err)
	}
	previous, ok := resolved.PreviousHistory(target.TargetID, target.TargetVersion)
	if !ok || previous.Response.Responses != nil || previous.Response.Interactions == nil || previous.Response.Interactions.ProviderInteractionID.String() != "interaction_previous" {
		t.Fatalf("Interactions PreviousHistory = (%#v, %t)", previous, ok)
	}
	if _, ok := resolved.PreviousHistory(target.TargetID+"-other", target.TargetVersion); ok {
		t.Fatal("target ID mismatch reused Interactions continuation")
	}
	if _, ok := resolved.PreviousHistory(target.TargetID, target.TargetVersion+1); ok {
		t.Fatal("target version mismatch reused Interactions continuation")
	}

	storeFalse := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: makeItems("turn two"),
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"}, Store: canonical.Specify(false),
	})
	stateless, err := Resume(storeFalse, record)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stateless.PreviousHistory(target.TargetID, target.TargetVersion); ok {
		t.Fatal("store:false exposed Interactions native continuation")
	}
}

func TestDraftFinalizeAllowsPreludePreparationAndRejectsHistoryRewrite(t *testing.T) {
	current := makeRequest("m", makeItems("one", "two", "three"), nil)
	draft, err := PrepareBegin(current)
	if err != nil {
		t.Fatal(err)
	}
	prepared := draft.Current().WithItems(append([]canonical.CanonicalItem{
		canonicaltest.MustInstruction(canonical.MessageRoleDeveloper, "prepared"),
	}, draft.Current().Items()...))
	resolved, err := draft.Finalize(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if got := canonicaltest.DirectiveText(resolved.Request().Items()); got != "prepared" {
		t.Fatalf("prepared directive = %q", got)
	}
	history := prepared.Items()
	cases := []struct {
		name    string
		mutated canonical.CanonicalRequest
	}{
		{name: "replace item", mutated: prepared.WithItems([]canonical.CanonicalItem{history[0], history[1], mustMessageItem(canonical.MessageRoleUser, "rewritten"), history[3]})},
		{name: "append item", mutated: prepared.WithItems(append(history, mustMessageItem(canonical.MessageRoleUser, "appended")))},
		{name: "remove item", mutated: prepared.WithItems(history[:len(history)-1])},
		{name: "reorder items", mutated: prepared.WithItems([]canonical.CanonicalItem{history[0], history[2], history[1], history[3]})},
		{name: "change model", mutated: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("other"), Items: history, ToolPolicy: prepared.ToolPolicyField(), ToolCallBatch: prepared.ToolCallBatchField(),
			Controls: prepared.Controls(), Reasoning: prepared.Reasoning(), OutputFormat: prepared.OutputFormatField(), Store: prepared.StoreField(),
		})},
		{name: "change controls", mutated: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: prepared.ModelField(), Items: history, ToolPolicy: prepared.ToolPolicyField(), ToolCallBatch: prepared.ToolCallBatchField(),
			Controls:  canonical.GenerationControls{Limits: canonical.GenerationLimits{MaxOutputTokens: canonical.NewOptionalInt(101)}},
			Reasoning: prepared.Reasoning(), OutputFormat: prepared.OutputFormatField(), Store: prepared.StoreField(),
		})},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := draft.Finalize(test.mutated); err == nil {
				t.Fatal("Finalize accepted forbidden mutation")
			}
		})
	}
}

func TestContinueAfterLocalResultPreservesHistoryAndRotatesAuthority(t *testing.T) {
	target := testBackendTarget(t, "m")
	previous := makeRequest("m", makeItems("turn one"), nil)
	resolved, err := Resume(
		makeRequest("m", makeItems("turn two"), &canonical.ResponseRef{SwobuID: "resp_previous"}),
		checkpoint("resp_previous", previous, makeResponse(mustMessageItem(canonical.MessageRoleAssistant, "answer")), nativeResponses(target, "provider_previous")),
	)
	if err != nil {
		t.Fatal(err)
	}
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"one"}`)))
	callID, err := canonical.NewToolCallID("call_1")
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	if err != nil {
		t.Fatal(err)
	}
	fresh := canonical.ResponseRef{SwobuID: "resp_fresh", Responses: nativeResponses(target, "provider_fresh")}
	providerResponse, err := canonical.NewCanonicalResponse(fresh, "m", []canonical.CanonicalItem{call}, canonical.Completed("requires_action"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	local, err := resolved.ContinueAfterLocalResult(providerResponse, []canonical.CanonicalItem{result})
	if err != nil {
		t.Fatal(err)
	}
	rotated, ok := local.PreviousHistory(target.TargetID, target.TargetVersion)
	if !ok || rotated.Response.Responses == nil || rotated.Response.Responses.ProviderResponseID.String() != "provider_fresh" {
		t.Fatalf("local MCP round previous history = (%#v, %t), want fresh provider response", rotated, ok)
	}
	if rotated.OmitStart != 0 || rotated.OmitEnd != uint32(len(resolved.Request().Items())+1) {
		t.Fatalf("local MCP round omit range = %d:%d, want prefix through provider call", rotated.OmitStart, rotated.OmitEnd)
	}
	if len(local.Request().Items()) != len(resolved.Request().Items())+2 {
		t.Fatal("local MCP round did not append complete history")
	}
	items := local.Request().Items()
	if appendedCall, ok := items[len(items)-2].ToolCall(); !ok || appendedCall.CallID() != callID {
		t.Fatal("local MCP call was not appended before its result")
	}
	if appendedResult, ok := items[len(items)-1].ToolResult(); !ok || appendedResult.CallID() != callID {
		t.Fatal("local MCP result lost call correlation")
	}
	foreignID, _ := canonical.NewToolCallID("call_other")
	foreign, _ := canonical.NewToolResultItem(foreignID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	if _, err := resolved.ContinueAfterLocalResult(providerResponse, []canonical.CanonicalItem{foreign}); err == nil {
		t.Fatal("local MCP round accepted a foreign result")
	}
	if _, err := resolved.ContinueAfterLocalResult(providerResponse, []canonical.CanonicalItem{result, result}); err == nil {
		t.Fatal("local MCP round accepted a duplicate result")
	}
}

func TestContinueAfterLocalResultReplacesInheritedInteractionsAuthority(t *testing.T) {
	target := testBackendTarget(t, "m")
	previous := makeRequest("m", makeItems("turn one"), nil)
	resolved, err := Resume(
		makeRequest("m", makeItems("turn two"), &canonical.ResponseRef{SwobuID: "resp_previous"}),
		interactionsCheckpoint("resp_previous", previous, makeResponse(mustMessageItem(canonical.MessageRoleAssistant, "answer")), nativeInteractions(target, "interaction_previous")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := resolved.PreviousHistory(target.TargetID, target.TargetVersion); !ok {
		t.Fatal("matching Interactions continuation was not available before local MCP round")
	}
	callID, err := canonical.NewToolCallID("call_1")
	if err != nil {
		t.Fatal(err)
	}
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	call := canonicaltest.ToolCall(t, callID.String(), key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"one"}`)))
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	if err != nil {
		t.Fatal(err)
	}
	fresh := canonical.ResponseRef{SwobuID: "resp_current", Interactions: nativeInteractions(target, "interaction_current")}
	providerResponse, err := canonical.NewCanonicalResponse(fresh, "m", []canonical.CanonicalItem{call}, canonical.Completed("requires_action"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	local, err := resolved.ContinueAfterLocalResult(providerResponse, []canonical.CanonicalItem{result})
	if err != nil {
		t.Fatal(err)
	}
	rotated, ok := local.PreviousHistory(target.TargetID, target.TargetVersion)
	if !ok || rotated.Response.Interactions == nil || rotated.Response.Interactions.ProviderInteractionID.String() != "interaction_current" {
		t.Fatalf("local MCP round previous history = (%#v, %t), want fresh Interactions continuation", rotated, ok)
	}
}
