package replay

import (
	"context"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
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
	backend := provider.Backend{Target: testBackendTarget(t, model)}
	if continuation {
		backend.CaptureContinuation = backend.Target.NativeContinuation
	}
	return backend
}

func makeRequest(model string, items []canonical.CanonicalItem, turn canonical.TurnRef) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: model, Items: items, Turn: turn,
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
		idSuffix = items[0].Text
	}
	return canonical.NewConversationOutput("resp_"+idSuffix, "gpt-4o", items, "stop")
}

func replayRecord(id ID, request canonical.CanonicalRequest, response canonical.CanonicalOutputProjection, native *provider.NativeContinuation) Record {
	return Record{ID: id, Request: request, Response: response, Native: native}
}

func TestPrepareWithoutPreviousIDDoesNotReadStore(t *testing.T) {
	spy := newSpyStore()
	request := makeRequest("gpt-4o", makeItems("hello"), canonical.TurnRef{})

	prepared, err := Prepare(context.Background(), spy, testWorkspaceSlug, request)
	if err != nil {
		t.Fatal(err)
	}
	if spy.getCalled {
		t.Fatal("store.Get called without previous_response_id")
	}
	providerRequest := prepared.ForBackend(testBackend(t, "gpt-4o", false), delivery.BufferedDelivery())
	if providerRequest.Continuation != nil {
		t.Fatalf("continuation = %#v, want nil", providerRequest.Continuation)
	}
	if len(providerRequest.Canonical.Items()) != 1 || providerRequest.Canonical.Items()[0].Text != "hello" {
		t.Fatalf("provider request items = %#v", providerRequest.Canonical.Items())
	}
}

func TestPrepareUnknownPreviousIDRejectsAfterOneLookup(t *testing.T) {
	spy := newSpyStore()
	request := makeRequest("gpt-4o", makeItems("hello"), canonical.NewTurnRef("resp_unknown"))

	_, err := Prepare(context.Background(), spy, testWorkspaceSlug, request)
	if err == nil || err.Error() != "BAD_REQUEST: unknown previous_response_id" {
		t.Fatalf("error = %v", err)
	}
	if spy.getCalls != 1 {
		t.Fatalf("store.Get calls = %d, want 1", spy.getCalls)
	}
}

func TestPrepareExpiredPreviousIDRejects(t *testing.T) {
	spy := newSpyStore()
	expiredAt := time.Now().UTC().Add(-time.Minute)
	record := replayRecord(
		"resp_expired",
		makeRequest("gpt-4o", makeItems("turn1"), canonical.TurnRef{}),
		makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")),
		testBackendTarget(t, "gpt-4o").NativeContinuation("provider_expired"),
	)
	record.ExpiresAt = &expiredAt
	if err := spy.Put(context.Background(), testWorkspaceSlug, record); err != nil {
		t.Fatal(err)
	}

	_, err := Prepare(context.Background(), spy, testWorkspaceSlug, makeRequest("gpt-4o", makeItems("turn2"), canonical.NewTurnRef("resp_expired")))
	if err == nil || err.Error() != "BAD_REQUEST: unknown previous_response_id" {
		t.Fatalf("error = %v", err)
	}
}

func TestPreparedMatchingBackendUsesInheritedDeltaAndNativeContinuation(t *testing.T) {
	spy := newSpyStore()
	backend := testBackendTarget(t, "gpt-4o")
	previous := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "gpt-4o", Instructions: "be concise", Items: makeItems("turn1"),
		Tools: []canonical.ToolDecl{canonical.NewFunctionToolDecl("search", "search", "search", canonical.NewToolSchemaObject(`{"type":"object"}`))},
	})
	record := replayRecord(
		"resp_prev", previous,
		makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")),
		backend.NativeContinuation("provider_prev"),
	)
	if err := spy.Put(context.Background(), testWorkspaceSlug, record); err != nil {
		t.Fatal(err)
	}
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: makeItems("turn2"), Turn: canonical.NewTurnRef("resp_prev"),
	})

	prepared, err := Prepare(context.Background(), spy, testWorkspaceSlug, current)
	if err != nil {
		t.Fatal(err)
	}
	request := prepared.ForBackend(testBackend(t, "gpt-4o", true), delivery.StreamingDelivery(delivery.FramingSSE))
	if request.Continuation == nil || request.Continuation.ID != "provider_prev" || request.Continuation.TargetID != backend.TargetID || request.Continuation.TargetVersion != backend.TargetVersion {
		t.Fatalf("continuation = %#v", request.Continuation)
	}
	if request.Canonical.Model() != "gpt-4o" || request.Canonical.Instructions() != "be concise" || len(request.Canonical.Tools()) != 1 {
		t.Fatalf("durable bands were not inherited: %#v", request.Canonical)
	}
	items := request.Canonical.Items()
	if len(items) != 1 || items[0].Text != "turn2" {
		t.Fatalf("delta items = %#v", items)
	}
	semanticItems := prepared.Semantic.Items()
	if len(semanticItems) != 3 || semanticItems[0].Text != "turn1" || semanticItems[1].Text != "assistant1" || semanticItems[2].Text != "turn2" {
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

	prepared, err := PrepareFromRecord(current, replayRecord(
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

	prepared, err := PrepareFromRecord(current, replayRecord(
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

	inherited, err := PrepareFromRecord(canonical.NewCanonicalRequest(canonical.RequestParams{Items: makeItems("turn2")}), record)
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
	replaced, err := PrepareFromRecord(replacement, record)
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

func TestPreparedMatchingBackendWithoutContinuationOptInUsesFullSemanticRequest(t *testing.T) {
	for _, protocol := range []string{"chat_completions", "messages", "responses_without_opt_in"} {
		t.Run(protocol, func(t *testing.T) {
			backend := testBackendTarget(t, "gpt-4o")
			prepared := Prepared{
				Semantic: makeRequest("gpt-4o", makeItems("turn1", "assistant1", "turn2"), canonical.TurnRef{}),
				Delta:    makeRequest("gpt-4o", makeItems("turn2"), canonical.TurnRef{}),
				Base:     &Record{Native: backend.NativeContinuation("provider_prev")},
			}

			request := prepared.ForBackend(provider.Backend{Target: backend}, delivery.BufferedDelivery())

			if request.Continuation != nil {
				t.Fatalf("continuation = %#v, want nil without exact-backend opt-in", request.Continuation)
			}
			if got := len(request.Canonical.Items()); got != 3 {
				t.Fatalf("semantic item count = %d, want full history", got)
			}
		})
	}
}

func TestPreparedMismatchedBackendUsesFullSemanticRequest(t *testing.T) {
	spy := newSpyStore()
	previousBackend := testBackendTarget(t, "gpt-4o")
	previous := replayRecord(
		"resp_prev",
		makeRequest("gpt-4o", makeItems("turn1"), canonical.TurnRef{}),
		makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")),
		previousBackend.NativeContinuation("provider_prev"),
	)
	if err := spy.Put(context.Background(), testWorkspaceSlug, previous); err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(context.Background(), spy, testWorkspaceSlug, makeRequest("gpt-4o", makeItems("turn2"), canonical.NewTurnRef("resp_prev")))
	if err != nil {
		t.Fatal(err)
	}

	request := prepared.ForBackend(testBackend(t, "gpt-4o-fallback", true), delivery.BufferedDelivery())
	if request.Continuation != nil {
		t.Fatalf("continuation survived backend mismatch: %#v", request.Continuation)
	}
	items := request.Canonical.Items()
	if len(items) != 3 || items[0].Text != "turn1" || items[1].Text != "assistant1" || items[2].Text != "turn2" {
		t.Fatalf("full semantic items = %#v", items)
	}
}

func TestPreparedMismatchedTargetVersionUsesFullSemanticRequest(t *testing.T) {
	target := testBackendTarget(t, "gpt-4o")
	prepared := Prepared{
		Semantic: makeRequest("gpt-4o", makeItems("turn1", "assistant1", "turn2"), canonical.TurnRef{}),
		Delta:    makeRequest("gpt-4o", makeItems("turn2"), canonical.TurnRef{}),
		Base:     &Record{Native: target.NativeContinuation("provider_prev")},
	}
	newVersion := target
	newVersion.TargetVersion++
	backend := provider.Backend{Target: newVersion, CaptureContinuation: newVersion.NativeContinuation}

	request := prepared.ForBackend(backend, delivery.BufferedDelivery())
	if request.Continuation != nil {
		t.Fatalf("continuation survived target-version mismatch: %#v", request.Continuation)
	}
	if got := len(request.Canonical.Items()); got != 3 {
		t.Fatalf("semantic item count = %d, want full history", got)
	}
}

func TestPreparedValuesDoNotAliasStoredRecord(t *testing.T) {
	spy := newSpyStore()
	previous := replayRecord(
		"resp_prev",
		makeRequest("gpt-4o", makeItems("turn1"), canonical.TurnRef{}),
		makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")),
		nil,
	)
	if err := spy.Put(context.Background(), testWorkspaceSlug, previous); err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(context.Background(), spy, testWorkspaceSlug, makeRequest("gpt-4o", makeItems("turn2"), canonical.NewTurnRef("resp_prev")))
	if err != nil {
		t.Fatal(err)
	}
	prepared.Semantic.Items()[0].Text = "mutated"
	stored, found, err := spy.Get(context.Background(), testWorkspaceSlug, "resp_prev")
	if err != nil || !found {
		t.Fatalf("stored record = %#v, found=%v, err=%v", stored, found, err)
	}
	if stored.Request.Items()[0].Text != "turn1" {
		t.Fatalf("stored request aliased prepared semantic: %#v", stored.Request.Items())
	}
}
