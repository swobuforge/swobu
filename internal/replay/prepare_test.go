package replay

import (
	"context"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

const testWorkspaceSlug = "test-ns"

func testBackendTarget(t *testing.T, model string) provider.TargetSnapshot {
	t.Helper()
	target := provider.NewTargetSnapshot("target-"+model, "openai", "https://api.openai.com", "test", "responses", "")
	target.Model = model
	return target
}

func testBackend(t *testing.T, model string, continuation bool) provider.Backend {
	t.Helper()
	_ = continuation
	return provider.Backend{Target: testBackendTarget(t, model)}
}

func makeRequest(model string, items []canonical.CanonicalItem, previous *canonical.ResponseRef) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: model, Items: items, PreviousResponse: previous,
		Controls: canonical.GenerationControls{Limits: canonical.GenerationLimits{MaxOutputTokens: canonical.NewOptionalInt(100)}},
	})
}

func makeItems(texts ...string) []canonical.CanonicalItem {
	items := make([]canonical.CanonicalItem, 0, len(texts))
	for _, text := range texts {
		items = append(items, canonical.NewTextItem(canonical.ItemAuthorUser, text))
	}
	return items
}

func makeResponse(items ...canonical.CanonicalItem) canonical.CanonicalOutputProjection {
	idSuffix := "empty"
	if len(items) > 0 {
		idSuffix = canonicalItemText(items[0])
	}
	return canonical.NewConversationOutput(canonical.NewSwobuResponseID("resp_"+idSuffix), "gpt-4o", items, "stop")
}

func replayRecord(id canonical.SwobuResponseID, request canonical.CanonicalRequest, response canonical.CanonicalOutputProjection, responses *canonical.ResponsesNativeRef) Record {
	return Record{Request: request, Response: response.WithResponse(canonical.ResponseRef{SwobuID: id, Responses: responses})}
}

func nativeResponses(target provider.TargetSnapshot, providerResponseID string) *canonical.ResponsesNativeRef {
	return &canonical.ResponsesNativeRef{ProviderResponseID: canonical.NewResponsesNativeResponseID(providerResponseID), TargetID: target.TargetID, TargetVersion: target.TargetVersion}
}

func TestPrepareCurrentRemovesPreviousResponse(t *testing.T) {
	request := makeRequest("gpt-4o", makeItems("hello"), nil)

	prepared := PrepareCurrent(request)
	providerRequest := prepared.PreferredForTarget(testBackend(t, "gpt-4o", false).Target)
	if _, ok := providerRequest.PreviousResponse(); ok {
		t.Fatalf("previous response survived without replay")
	}
	if len(providerRequest.Items()) != 1 || canonicalItemText(providerRequest.Items()[0]) != "hello" {
		t.Fatalf("provider request items = %#v", providerRequest.Items())
	}
}

func TestPrepareExpiredPreviousIDRejects(t *testing.T) {
	expiredAt := time.Now().UTC().Add(-time.Minute)
	record := replayRecord(
		"resp_expired",
		makeRequest("gpt-4o", makeItems("turn1"), nil),
		makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")),
		nativeResponses(testBackendTarget(t, "gpt-4o"), "provider_expired"),
	)
	record.ExpiresAt = &expiredAt
	_, err := PrepareFromRecord(
		makeRequest("gpt-4o", makeItems("turn2"), &canonical.ResponseRef{SwobuID: "resp_expired"}),
		"resp_expired",
		record,
	)
	if err == nil || err.Error() != "BAD_REQUEST: unknown previous_response_id" {
		t.Fatalf("error = %v", err)
	}
}

func TestPreparedMatchingBackendUsesInheritedDeltaAndResponsesRefinement(t *testing.T) {
	backend := testBackendTarget(t, "gpt-4o")
	previous := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "gpt-4o", Instructions: "be concise", Items: makeItems("turn1"),
		Tools: []canonical.ToolDecl{canonical.NewFunctionToolDecl("search", "search", "search", canonical.NewToolSchemaObject(`{"type":"object"}`))},
	})
	record := replayRecord(
		"resp_prev", previous,
		makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")),
		nativeResponses(backend, "provider_prev"),
	)
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: makeItems("turn2"), PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_prev"},
	})

	prepared, err := PrepareFromRecord(current, "resp_prev", record)
	if err != nil {
		t.Fatal(err)
	}
	request := prepared.PreferredForTarget(testBackend(t, "gpt-4o", true).Target)
	previousRef, ok := request.PreviousResponse()
	if !ok || previousRef.Responses == nil || previousRef.Responses.ProviderResponseID != "provider_prev" || previousRef.Responses.TargetID != backend.TargetID || previousRef.Responses.TargetVersion != backend.TargetVersion {
		t.Fatalf("previous response = %#v", previousRef)
	}
	if request.Model() != "gpt-4o" || request.Instructions() != "be concise" || len(request.Tools()) != 1 {
		t.Fatalf("durable bands were not inherited: %#v", request)
	}
	items := request.Items()
	if len(items) != 1 || canonicalItemText(items[0]) != "turn2" {
		t.Fatalf("delta items = %#v", items)
	}
	semanticItems := prepared.Semantic.Items()
	if len(semanticItems) != 3 || canonicalItemText(semanticItems[0]) != "turn1" || canonicalItemText(semanticItems[1]) != "assistant1" || canonicalItemText(semanticItems[2]) != "turn2" {
		t.Fatalf("semantic items = %#v", semanticItems)
	}
}

func TestPrepareFromRecordExplicitEmptyCollectionsClearInheritedBands(t *testing.T) {
	previous := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "gpt-4o",
		Tools: []canonical.ToolDecl{
			canonical.NewFunctionToolDecl("search", "search", "search", canonical.NewToolSchemaObject(`{"type":"object"}`)),
		},
		Controls: canonical.GenerationControls{
			Limits: canonical.GenerationLimits{StopSequences: []string{"END"}},
		},
	})
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "gpt-4o",
		Items: makeItems("turn2"),
		Tools: []canonical.ToolDecl{},
		Controls: canonical.GenerationControls{
			Limits: canonical.GenerationLimits{StopSequences: []string{}},
		},
	})

	prepared, err := prepareFromRecordForTest(current, replayRecord(
		"resp_prev",
		previous,
		makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")),
		nil,
	))
	if err != nil {
		t.Fatal(err)
	}
	if got := prepared.Semantic.Tools(); len(got) != 0 {
		t.Fatalf("semantic tools = %#v, want explicit clear", got)
	}
	if got := prepared.Semantic.Controls().Limits.StopSequences; len(got) != 0 {
		t.Fatalf("semantic stop sequences = %#v, want explicit clear", got)
	}
}

func TestPrepareFromRecordExplicitZeroClearsEveryDurableBand(t *testing.T) {
	previous := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "gpt-4o",
		Instructions: "be concise",
		Tools: []canonical.ToolDecl{
			canonical.NewFunctionToolDecl("search", "search", "search", canonical.NewToolSchemaObject(`{"type":"object"}`)),
		},
		ToolPolicy:    canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
		ToolCallBatch: canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne),
		Controls: canonical.GenerationControls{
			Limits: canonical.GenerationLimits{
				MaxOutputTokens: canonical.NewOptionalInt(100),
				StopSequences:   []string{"END"},
			},
			Sampling: canonical.SamplingControls{
				Temperature: canonical.NewOptionalFloat64(0.7),
				TopP:        canonical.NewOptionalFloat64(0.9),
			},
		},
		OutputFormat: mustOutputFormat(t, canonical.OutputFormatParams{
			Kind:   canonical.OutputFormatJSONSchema,
			Name:   "previous",
			Schema: canonical.NewRawJSONObject(`{"type":"object"}`),
		}),
	})
	presence := canonical.RequestPresence{
		Model:         true,
		Instructions:  true,
		Tools:         true,
		ToolPolicy:    true,
		ToolCallBatch: true,
		OutputFormat:  true,
		Controls: canonical.GenerationControlsPresence{
			MaxOutputTokens: true,
			StopSequences:   true,
			Temperature:     true,
			TopP:            true,
		},
	}
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Items:    makeItems("turn2"),
		Tools:    []canonical.ToolDecl{},
		Controls: canonical.GenerationControls{Limits: canonical.GenerationLimits{StopSequences: []string{}}},
		Presence: presence,
	})

	prepared, err := prepareFromRecordForTest(current, replayRecord(
		"resp_prev",
		previous,
		makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")),
		nil,
	))
	if err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]canonical.CanonicalRequest{
		"semantic": prepared.Semantic,
		"delta":    prepared.Delta,
	} {
		if request.Model() != "" || request.Instructions() != "" || len(request.Tools()) != 0 {
			t.Fatalf("%s scalar/collection clears failed: model=%q instructions=%q tools=%#v", name, request.Model(), request.Instructions(), request.Tools())
		}
		if !request.ToolPolicy().IsZero() || !request.ToolCallBatch().IsZero() || !request.OutputFormat().IsZero() {
			t.Fatalf("%s policy/format clears failed: policy=%#v batch=%#v format=%#v", name, request.ToolPolicy(), request.ToolCallBatch(), request.OutputFormat())
		}
		controls := request.Controls()
		if !controls.Limits.MaxOutputTokens.IsZero() || len(controls.Limits.StopSequences) != 0 || !controls.Sampling.Temperature.IsZero() || !controls.Sampling.TopP.IsZero() {
			t.Fatalf("%s generation clears failed: %#v", name, controls)
		}
	}
}

func TestPrepareFromRecordAbsentInheritsAndNonEmptyReplacesDurableBands(t *testing.T) {
	previous := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         "previous-model",
		Instructions:  "previous instructions",
		Tools:         []canonical.ToolDecl{canonical.NewFunctionToolDecl("old", "old", "old", canonical.NewToolSchemaObject(`{"type":"object"}`))},
		ToolPolicy:    canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
		ToolCallBatch: canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne),
		Controls: canonical.GenerationControls{
			Limits:   canonical.GenerationLimits{MaxOutputTokens: canonical.NewOptionalInt(100), StopSequences: []string{"OLD"}},
			Sampling: canonical.SamplingControls{Temperature: canonical.NewOptionalFloat64(0.2), TopP: canonical.NewOptionalFloat64(0.3)},
		},
		OutputFormat: mustOutputFormat(t, canonical.OutputFormatParams{
			Kind:   canonical.OutputFormatJSONSchema,
			Name:   "previous",
			Schema: canonical.NewRawJSONObject(`{"type":"object"}`),
		}),
	})
	record := replayRecord("resp_prev", previous, makeResponse(), nil)

	inherited, err := prepareFromRecordForTest(canonical.NewCanonicalRequest(canonical.RequestParams{Items: makeItems("turn2")}), record)
	if err != nil {
		t.Fatal(err)
	}
	assertPreparedDurableBands(t, inherited, "previous-model", "previous instructions", "old", canonical.ToolPolicyRequired, canonical.ToolCallBatchAtMostOne, 100, "OLD", 0.2, 0.3, canonical.OutputFormatJSONSchema)

	replacement := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         "replacement-model",
		Instructions:  "replacement instructions",
		Items:         makeItems("turn2"),
		Tools:         []canonical.ToolDecl{canonical.NewFunctionToolDecl("new", "new", "new", canonical.NewToolSchemaObject(`{"type":"object"}`))},
		ToolPolicy:    canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil),
		ToolCallBatch: canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne),
		Controls: canonical.GenerationControls{
			Limits:   canonical.GenerationLimits{MaxOutputTokens: canonical.NewOptionalInt(200), StopSequences: []string{"NEW"}},
			Sampling: canonical.SamplingControls{Temperature: canonical.NewOptionalFloat64(0.7), TopP: canonical.NewOptionalFloat64(0.8)},
		},
		OutputFormat: canonical.OutputFormat{Kind: canonical.OutputFormatText},
	})
	replaced, err := prepareFromRecordForTest(replacement, record)
	if err != nil {
		t.Fatal(err)
	}
	assertPreparedDurableBands(t, replaced, "replacement-model", "replacement instructions", "new", canonical.ToolPolicyAuto, canonical.ToolCallBatchAtMostOne, 200, "NEW", 0.7, 0.8, canonical.OutputFormatText)
}

func mustOutputFormat(t *testing.T, params canonical.OutputFormatParams) canonical.OutputFormat {
	t.Helper()
	format, err := canonical.NewOutputFormat(params)
	if err != nil {
		t.Fatal(err)
	}
	return format
}

func prepareFromRecordForTest(request canonical.CanonicalRequest, record Record) (Prepared, error) {
	return PrepareFromRecord(request, record.Response.Response().SwobuID, record)
}

func canonicalItemText(item canonical.CanonicalItem) string {
	if text, ok := item.TextItem(); ok {
		return text.Text
	}
	if toolResult, ok := item.ToolResult(); ok {
		return toolResult.Text
	}
	return ""
}

func assertPreparedDurableBands(t *testing.T, prepared Prepared, model, instructions, toolName string, policy canonical.ToolPolicyMode, batch canonical.ToolCallBatchMode, maxTokens int, stop string, temperature, topP float64, format canonical.OutputFormatKind) {
	t.Helper()
	for name, request := range map[string]canonical.CanonicalRequest{"semantic": prepared.Semantic, "delta": prepared.Delta} {
		if request.Model() != model || request.Instructions() != instructions {
			t.Fatalf("%s model/instructions = %q/%q, want %q/%q", name, request.Model(), request.Instructions(), model, instructions)
		}
		tools := request.Tools()
		if len(tools) != 1 || tools[0].ToolName() != toolName {
			t.Fatalf("%s tools = %#v, want %q", name, tools, toolName)
		}
		if request.ToolPolicy().Mode != policy || request.ToolCallBatch().Mode != batch || request.OutputFormat().Kind != format {
			t.Fatalf("%s policy/batch/format = %#v/%#v/%#v", name, request.ToolPolicy(), request.ToolCallBatch(), request.OutputFormat())
		}
		controls := request.Controls()
		gotMax, maxOK := controls.Limits.MaxOutputTokens.Value()
		gotTemp, tempOK := controls.Sampling.Temperature.Value()
		gotTopP, topPOK := controls.Sampling.TopP.Value()
		if !maxOK || gotMax != maxTokens || len(controls.Limits.StopSequences) != 1 || controls.Limits.StopSequences[0] != stop || !tempOK || gotTemp != temperature || !topPOK || gotTopP != topP {
			t.Fatalf("%s controls = %#v", name, controls)
		}
	}
}

func TestPreparedWithoutResponsesRefinementUsesFullSemanticRequest(t *testing.T) {
	for _, protocol := range []string{"chat_completions", "messages", "responses_without_opt_in"} {
		t.Run(protocol, func(t *testing.T) {
			prepared := Prepared{
				Semantic: makeRequest("gpt-4o", makeItems("turn1", "assistant1", "turn2"), nil),
				Delta:    makeRequest("gpt-4o", makeItems("turn2"), nil),
			}

			request := prepared.PreferredForTarget(testBackendTarget(t, "gpt-4o"))

			if _, ok := request.PreviousResponse(); ok {
				t.Fatal("previous response survived without Responses refinement")
			}
			if got := len(request.Items()); got != 3 {
				t.Fatalf("semantic item count = %d, want full history", got)
			}
		})
	}
}

func TestPreparedMismatchedBackendUsesFullSemanticRequest(t *testing.T) {
	previousBackend := testBackendTarget(t, "gpt-4o")
	previous := replayRecord(
		"resp_prev",
		makeRequest("gpt-4o", makeItems("turn1"), nil),
		makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")),
		nativeResponses(previousBackend, "provider_prev"),
	)
	prepared, err := PrepareFromRecord(
		makeRequest("gpt-4o", makeItems("turn2"), &canonical.ResponseRef{SwobuID: "resp_prev"}),
		"resp_prev",
		previous,
	)
	if err != nil {
		t.Fatal(err)
	}

	request := prepared.PreferredForTarget(testBackend(t, "gpt-4o-fallback", true).Target)
	if _, ok := request.PreviousResponse(); ok {
		t.Fatal("previous response survived backend mismatch")
	}
	items := request.Items()
	if len(items) != 3 || canonicalItemText(items[0]) != "turn1" || canonicalItemText(items[1]) != "assistant1" || canonicalItemText(items[2]) != "turn2" {
		t.Fatalf("full semantic items = %#v", items)
	}
}

func TestPreparedMismatchedTargetVersionUsesFullSemanticRequest(t *testing.T) {
	target := testBackendTarget(t, "gpt-4o")
	prepared := Prepared{
		Semantic: makeRequest("gpt-4o", makeItems("turn1", "assistant1", "turn2"), nil),
		Delta: makeRequest("gpt-4o", makeItems("turn2"), &canonical.ResponseRef{
			SwobuID: "resp_prev", Responses: nativeResponses(target, "provider_prev"),
		}),
	}
	newVersion := target
	newVersion.TargetVersion++
	request := prepared.PreferredForTarget(newVersion)
	if _, ok := request.PreviousResponse(); ok {
		t.Fatal("previous response survived target-version mismatch")
	}
	if got := len(request.Items()); got != 3 {
		t.Fatalf("semantic item count = %d, want full history", got)
	}
}

func TestPreparedValuesDoNotAliasStoredRecord(t *testing.T) {
	spy := newSpyStore()
	previous := replayRecord(
		"resp_prev",
		makeRequest("gpt-4o", makeItems("turn1"), nil),
		makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")),
		nil,
	)
	if err := spy.Put(context.Background(), testWorkspaceSlug, previous); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareFromRecord(
		makeRequest("gpt-4o", makeItems("turn2"), &canonical.ResponseRef{SwobuID: "resp_prev"}),
		"resp_prev",
		previous,
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := prepared.Semantic.Items()[0].TextItem()
	payload.Text = "mutated"
	stored, found, err := spy.Get(context.Background(), testWorkspaceSlug, "resp_prev")
	if err != nil || !found {
		t.Fatalf("stored record = %#v, found=%v, err=%v", stored, found, err)
	}
	if canonicalItemText(stored.Request.Items()[0]) != "turn1" {
		t.Fatalf("stored request aliased prepared semantic: %#v", stored.Request.Items())
	}
}
