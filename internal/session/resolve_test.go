package session

import (
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

func checkpoint(id canonical.SwobuResponseID, request canonical.CanonicalRequest, response canonical.CanonicalResponse, responses *canonical.ResponsesNativeRef) Checkpoint {
	bound, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id, Responses: responses}, response.Model(), response.Items(), response.CompletionReason(), response.Usage())
	if err != nil {
		panic(err)
	}
	return Checkpoint{Request: request, Response: bound}
}

func nativeResponses(target provider.TargetSnapshot, providerResponseID string) *canonical.ResponsesNativeRef {
	return &canonical.ResponsesNativeRef{ProviderResponseID: canonical.NewResponsesNativeResponseID(providerResponseID), TargetID: target.TargetID, TargetVersion: target.TargetVersion}
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

func TestBeginMaterializesEveryDefaultBearingBand(t *testing.T) {
	source := canonical.NewCanonicalRequest(canonical.RequestParams{})
	prepared, err := Begin(source)
	if err != nil {
		t.Fatal(err)
	}
	semantic := prepared.Full
	for name, specified := range map[string]bool{
		"model":              semantic.ModelSpecified(),
		"instructions":       semantic.InstructionsSpecified(),
		"tools":              semantic.ToolsSpecified(),
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
		"instructions":       prepared.Delta.InstructionsSpecified(),
		"tools":              prepared.Delta.ToolsSpecified(),
		"tool policy":        prepared.Delta.ToolPolicySpecified(),
		"tool-call batching": prepared.Delta.ToolCallBatchSpecified(),
		"output format":      prepared.Delta.OutputFormatSpecified(),
	} {
		if specified {
			t.Fatalf("source delta changed omitted %s into explicit empty", name)
		}
	}
	emptyInstructions, _ := canonical.NewInstructionSet(nil)
	emptyTools, _ := canonical.NewToolSet(nil)
	explicit := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(""), Instructions: canonical.Specify(emptyInstructions), Tools: canonical.Specify(emptyTools),
		ToolPolicy: canonical.Specify(canonical.ToolPolicy{}), ToolCallBatch: canonical.Specify(canonical.ToolCallBatchPolicy{}), OutputFormat: canonical.Specify(canonical.OutputFormat{}),
	})
	explicitPrepared, err := Begin(explicit)
	if err != nil {
		t.Fatal(err)
	}
	explicitDelta := explicitPrepared.Delta
	for name, specified := range map[string]bool{
		"model":              explicitDelta.ModelSpecified(),
		"instructions":       explicitDelta.InstructionsSpecified(),
		"tools":              explicitDelta.ToolsSpecified(),
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
		Model: canonical.Specify("old"), Instructions: canonical.Specify(canonical.NewSystemInstructionSet("concise")),
		Tools: canonical.Specify(mustTestToolSet(t, tool)), ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)),
		ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)), OutputFormat: canonical.Specify(structured),
		Controls: canonical.GenerationControls{Limits: canonical.GenerationLimits{StopSequences: []string{"END"}}},
	})
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(""), Instructions: canonical.Specify(canonical.InstructionSet{}), Items: makeItems("turn2"),
		Tools:            canonical.Specify(mustTestToolSet(t)),
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
	if prepared.Full.Model() != "" || canonicaltest.InstructionSetText(prepared.Full.Instructions()) != "" || len(prepared.Full.Tools()) != 0 || prepared.Full.Controls().Limits.StopSequences == nil || prepared.Full.ToolPolicy().Mode != canonical.ToolPolicyNone || prepared.Full.ToolCallBatch().Mode != canonical.ToolCallBatchUnspecified || prepared.Full.OutputFormat().Kind != canonical.OutputFormatText {
		t.Fatalf("explicit clears not retained: %#v", prepared.Full)
	}
}

func TestResumeInheritsUnspecifiedBands(t *testing.T) {
	previous := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("old"), Instructions: canonical.Specify(canonical.NewSystemInstructionSet("concise"))})
	current := canonical.NewCanonicalRequest(canonical.RequestParams{Items: makeItems("turn2"), PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_prev"}})
	prepared, err := Resume(current, checkpoint("resp_prev", previous, makeResponse(), nil))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Full.Model() != "old" || canonicaltest.InstructionSetText(prepared.Full.Instructions()) != "concise" {
		t.Fatalf("bands not inherited: model=%q instructions=%q", prepared.Full.Model(), canonicaltest.InstructionSetText(prepared.Full.Instructions()))
	}
}

func TestResolvedRequestUsesDeltaOnlyForExactTargetGeneration(t *testing.T) {
	target := testBackendTarget(t, "gpt-4o")
	prepared := ResolvedRequest{
		Full:  makeRequest("gpt-4o", makeItems("turn1", "assistant1", "turn2"), nil),
		Delta: makeRequest("gpt-4o", makeItems("turn2"), &canonical.ResponseRef{SwobuID: "resp_prev", Responses: nativeResponses(target, "provider_prev")}),
	}
	if got := prepared.ForTarget(target); len(got.Items()) != 1 {
		t.Fatalf("exact target items=%d, want delta", len(got.Items()))
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
