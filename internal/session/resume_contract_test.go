package session

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestResumeStoresCompleteRequestAndReturnsTargetGatedResponsesData(t *testing.T) {
	target := testBackendTarget(t, "m")
	previous := makeRequest("m", makeItems("turn one"), nil)
	record := checkpoint("resp_previous", previous, makeResponse(
		mustMessageItem(canonical.MessageRoleAssistant, "answer one"),
	), nativeResponses(target, "provider_previous"))
	resolved, err := Resume(makeRequest("m", makeItems("turn two"), &canonical.ResponseRef{SwobuID: "resp_previous"}), record)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(resolved.Request().Items()); got != 3 {
		t.Fatalf("complete request items = %d, want 3", got)
	}
	id, start, end, ok := resolved.ResponsesPrevious(target.TargetID, target.TargetVersion)
	if !ok || id.String() != "provider_previous" || start != 0 || end != 2 {
		t.Fatalf("ResponsesPrevious = (%q, %d, %d, %t)", id, start, end, ok)
	}
	if _, _, _, ok := resolved.ResponsesPrevious(target.TargetID+"-other", target.TargetVersion); ok {
		t.Fatal("target ID mismatch reused Responses continuation")
	}
	if _, _, _, ok := resolved.ResponsesPrevious(target.TargetID, target.TargetVersion+1); ok {
		t.Fatal("target version mismatch reused Responses continuation")
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
			Controls: prepared.Controls(), Reasoning: prepared.Reasoning(), OutputFormat: prepared.OutputFormatField(), Responses: prepared.Responses(),
		})},
		{name: "change controls", mutated: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: prepared.ModelField(), Items: history, ToolPolicy: prepared.ToolPolicyField(), ToolCallBatch: prepared.ToolCallBatchField(),
			Controls:  canonical.GenerationControls{Limits: canonical.GenerationLimits{MaxOutputTokens: canonical.NewOptionalInt(101)}},
			Reasoning: prepared.Reasoning(), OutputFormat: prepared.OutputFormatField(), Responses: prepared.Responses(),
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

func TestAppendLocalRoundValidatesToolCorrelationAndClearsResponsesContinuation(t *testing.T) {
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
	local, err := resolved.AppendLocalRound(
		[]canonical.CanonicalItem{call},
		[]canonical.CanonicalItem{result},
	)
	if err != nil {
		t.Fatal(err)
	}
	if local.HasResponsesPrevious() {
		t.Fatal("local MCP round retained provider continuation")
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
	if _, err := resolved.AppendLocalRound([]canonical.CanonicalItem{call}, []canonical.CanonicalItem{foreign}); err == nil {
		t.Fatal("local MCP round accepted a foreign result")
	}
	if _, err := resolved.AppendLocalRound([]canonical.CanonicalItem{call}, []canonical.CanonicalItem{result, result}); err == nil {
		t.Fatal("local MCP round accepted a duplicate result")
	}
}
