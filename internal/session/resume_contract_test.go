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
	current := makeRequest("m", makeItems("current"), nil)
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
	changed := prepared.Items()
	changed[len(changed)-1] = mustMessageItem(canonical.MessageRoleUser, "rewritten")
	if _, err := draft.Finalize(prepared.WithItems(changed)); err == nil {
		t.Fatal("Finalize accepted rewritten current history")
	}
}

func TestAppendLocalRoundClearsResponsesContinuation(t *testing.T) {
	target := testBackendTarget(t, "m")
	previous := makeRequest("m", makeItems("turn one"), nil)
	resolved, err := Resume(
		makeRequest("m", makeItems("turn two"), &canonical.ResponseRef{SwobuID: "resp_previous"}),
		checkpoint("resp_previous", previous, makeResponse(mustMessageItem(canonical.MessageRoleAssistant, "answer")), nativeResponses(target, "provider_previous")),
	)
	if err != nil {
		t.Fatal(err)
	}
	local, err := resolved.AppendLocalRound(
		[]canonical.CanonicalItem{mustMessageItem(canonical.MessageRoleAssistant, "call")},
		[]canonical.CanonicalItem{mustMessageItem(canonical.MessageRoleUser, "result")},
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
}
