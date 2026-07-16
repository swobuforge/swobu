package replay

import (
	"context"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

var testScope = Scope{Namespace: "test-ns", CallerKey: "test-caller"}

func testTarget() TargetKey {
	return TargetKey{
		ProviderSpec:     "openai",
		Protocol:         protocolkind.Responses,
		ProviderProtocol: "responses",
		BaseURL:          "https://api.openai.com",
		AuthScope:        "default",
		ModelID:          "gpt-4o",
	}
}

func testTargetDifferent() TargetKey {
	return TargetKey{
		ProviderSpec:     "anthropic",
		Protocol:         protocolkind.Messages,
		ProviderProtocol: "messages",
		BaseURL:          "https://api.anthropic.com",
		AuthScope:        "default",
		ModelID:          "claude-3-sonnet",
	}
}

func testTargetPtr() *TargetKey {
	target := testTarget()
	return &target
}

func testTargetDifferentPtr() *TargetKey {
	target := testTargetDifferent()
	return &target
}

func makeRequest(model string, items []canonical.CanonicalItem, turn canonical.TurnRef) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:    model,
		Items:    items,
		Turn:     turn,
		Controls: canonical.GenerationControls{Limits: canonical.GenerationLimits{MaxOutputTokens: canonical.NewOptionalInt(100)}},
	})
}

func makeItems(texts ...string) []canonical.CanonicalItem {
	items := make([]canonical.CanonicalItem, 0, len(texts))
	for _, t := range texts {
		items = append(items, canonical.NewTextItem(canonical.ItemAuthorUser, t))
	}
	return items
}

func makeRecord(id ID, target TargetKey, request canonical.CanonicalRequest, response canonical.CanonicalOutputProjection, native *NativeRef) Record {
	return Record{
		ID:       id,
		Scope:    testScope,
		Request:  request,
		Response: response,
		Native:   native,
	}
}

func makeResponse(items ...canonical.CanonicalItem) canonical.CanonicalOutputProjection {
	idSuffix := "empty"
	if len(items) > 0 {
		idSuffix = items[0].Text
	}
	return canonical.NewConversationOutput("resp_"+idSuffix, "gpt-4o", items, "stop")
}

// --- Prepare tests ---

func TestPrepareNoPreviousReturnsRequestAndNilNative(t *testing.T) {
	spy := newSpyStore()
	req := makeRequest("gpt-4o", makeItems("hello"), canonical.TurnRef{})

	got, native, err := Prepare(context.Background(), spy, testScope, testTargetPtr(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spy.getCalled {
		t.Error("expected store.Get not to be called when request has no previous ID")
	}
	if native != nil {
		t.Errorf("expected nil native ref, got %+v", native)
	}
	if !got.Turn().IsZero() {
		t.Errorf("expected turn to be cleared, got %+v", got.Turn())
	}
	items := got.Items()
	if len(items) != 1 || items[0].Text != "hello" {
		t.Errorf("expected [hello], got %+v", items)
	}
}

func TestPrepareUnknownPreviousRejects(t *testing.T) {
	spy := newSpyStore()
	turn := canonical.NewTurnRef("resp_unknown")
	req := makeRequest("gpt-4o", makeItems("hello"), turn)

	_, _, err := Prepare(context.Background(), spy, testScope, testTargetPtr(), req)
	if err == nil {
		t.Fatal("expected error for unknown previous response id, got nil")
	}
	if err.Error() != "BAD_REQUEST: unknown previous_response_id" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPrepareExpiredPreviousRejects(t *testing.T) {
	spy := newSpyStore()
	expiredAt := time.Now().UTC().Add(-time.Minute)
	record := Record{
		ID:        "resp_expired",
		Scope:     testScope,
		Request:   makeRequest("gpt-4o", makeItems("turn1"), canonical.TurnRef{}),
		Response:  makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")),
		Native:    &NativeRef{ReplayID: "resp_expired", Target: testTarget(), Kind: NativeRefProviderResponseID, Value: "provider_expired"},
		CreatedAt: time.Now().UTC().Add(-2 * time.Minute),
		ExpiresAt: &expiredAt,
	}
	if err := spy.Put(context.Background(), testScope, record); err != nil {
		t.Fatalf("seed expired record: %v", err)
	}

	req := makeRequest("gpt-4o", makeItems("turn2"), canonical.NewTurnRef("resp_expired"))
	_, _, err := Prepare(context.Background(), spy, testScope, testTargetPtr(), req)
	if err == nil {
		t.Fatal("expected expired previous_response_id to reject")
	}
	if err.Error() != "BAD_REQUEST: unknown previous_response_id" {
		t.Fatalf("error = %v, want BAD_REQUEST: unknown previous_response_id", err)
	}
}

func TestPrepareSameTargetReturnsDeltaAndNativeRef(t *testing.T) {
	spy := newSpyStore()
	prevItems := makeItems("turn1")
	prevReq := makeRequest("gpt-4o", prevItems, canonical.TurnRef{})
	prevResp := makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1"))
	nativeRef := &NativeRef{
		ReplayID: "resp_prev",
		Target:   testTarget(),
		Kind:     NativeRefProviderResponseID,
		Value:    "provider_prev_id",
	}
	record := makeRecord("resp_prev", testTarget(), prevReq, prevResp, nativeRef)
	spy.Put(context.Background(), testScope, record)

	turn := canonical.NewTurnRef("resp_prev")
	req := makeRequest("gpt-4o", makeItems("turn2"), turn)

	got, native, err := Prepare(context.Background(), spy, testScope, testTargetPtr(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if native == nil {
		t.Fatal("expected native ref for same target, got nil")
	}
	if native.Value != "provider_prev_id" {
		t.Errorf("expected native value provider_prev_id, got %s", native.Value)
	}
	if !got.Turn().IsZero() {
		t.Fatalf("expected same-target native replay request to clear turn, got %+v", got.Turn())
	}
	items := got.Items()
	if len(items) != 1 || items[0].Text != "turn2" {
		t.Errorf("expected delta [turn2], got %+v", items)
	}
}

func TestPrepareSameTargetNativeInheritsMissingBands(t *testing.T) {
	spy := newSpyStore()
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: intPtr(256),
		StopSequences:   []string{"STOP"},
		Temperature:     floatPtr(0.3),
		TopP:            floatPtr(0.8),
	})
	if err != nil {
		t.Fatalf("construct controls: %v", err)
	}
	outputFormat, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:        canonical.OutputFormatJSONSchema,
		Name:        "reply_shape",
		Description: "structured reply",
		Schema:      canonical.NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`),
		Strict:      true,
	})
	if err != nil {
		t.Fatalf("construct output format: %v", err)
	}
	prevReq := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "gpt-4o",
		Instructions: "You are helpful",
		Items:        []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn1")},
		Tools: []canonical.ToolDecl{
			canonical.NewFunctionToolDecl("search", "search", "search text", canonical.NewToolSchemaObject(`{"type":"object","properties":{"q":{"type":"string"}}}`)),
		},
		ToolPolicy:    canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
		ToolCallBatch: canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne),
		Controls:      controls,
		OutputFormat:  outputFormat,
	})
	prevResp := canonical.NewConversationOutput("resp_prev", "gpt-4o", []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")}, "stop")
	nativeRef := &NativeRef{
		ReplayID: "resp_prev",
		Target:   testTarget(),
		Kind:     NativeRefProviderResponseID,
		Value:    "provider_prev_id",
	}
	record := makeRecord("resp_prev", testTarget(), prevReq, prevResp, nativeRef)
	spy.Put(context.Background(), testScope, record)

	turn := canonical.NewTurnRef("resp_prev")
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn2")},
		Turn:  turn,
	})

	got, native, err := Prepare(context.Background(), spy, testScope, testTargetPtr(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if native == nil {
		t.Fatal("expected native ref for same target, got nil")
	}
	if got.Turn().IsZero() == false {
		t.Fatalf("expected native replay request to clear turn, got %+v", got.Turn())
	}
	if got.Model() != "gpt-4o" {
		t.Fatalf("expected inherited model gpt-4o, got %s", got.Model())
	}
	if got.Instructions() != "You are helpful" {
		t.Fatalf("expected inherited instructions, got %s", got.Instructions())
	}
	if len(got.Tools()) != 1 {
		t.Fatalf("expected 1 inherited tool, got %d", len(got.Tools()))
	}
	if got.ToolPolicy().Mode != canonical.ToolPolicyRequired {
		t.Fatalf("expected inherited tool policy required, got %v", got.ToolPolicy().Mode)
	}
	if got.ToolCallBatch().Mode != canonical.ToolCallBatchAtMostOne {
		t.Fatalf("expected inherited tool batch policy at_most_one, got %v", got.ToolCallBatch().Mode)
	}
	if gotControls := got.Controls(); gotControls.Limits.MaxOutputTokens.IsZero() {
		t.Fatal("expected inherited max_output_tokens")
	} else if value, _ := gotControls.Limits.MaxOutputTokens.Value(); value != 256 {
		t.Fatalf("expected inherited max_output_tokens 256, got %d", value)
	}
	if gotControls := got.Controls(); gotControls.Sampling.Temperature.IsZero() {
		t.Fatal("expected inherited temperature")
	} else if value, _ := gotControls.Sampling.Temperature.Value(); value != 0.3 {
		t.Fatalf("expected inherited temperature 0.3, got %v", value)
	}
	if gotControls := got.Controls(); gotControls.Sampling.TopP.IsZero() {
		t.Fatal("expected inherited top_p")
	} else if value, _ := gotControls.Sampling.TopP.Value(); value != 0.8 {
		t.Fatalf("expected inherited top_p 0.8, got %v", value)
	}
	if gotFormat := got.OutputFormat(); gotFormat.Kind != canonical.OutputFormatJSONSchema || gotFormat.Name != "reply_shape" || gotFormat.Description != "structured reply" || gotFormat.Schema.RawObject() != `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}` || !gotFormat.Strict {
		t.Fatalf("expected inherited output format, got %+v", gotFormat)
	}
	items := got.Items()
	if len(items) != 1 || items[0].Text != "turn2" {
		t.Fatalf("expected delta [turn2], got %+v", items)
	}
}

func TestPrepareDifferentTargetReturnsFullRequestAndNilNative(t *testing.T) {
	spy := newSpyStore()
	prevItems := makeItems("turn1")
	prevReq := makeRequest("gpt-4o", prevItems, canonical.TurnRef{})
	prevResp := makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1"))
	nativeRef := &NativeRef{
		ReplayID: "resp_prev",
		Target:   testTarget(),
		Kind:     NativeRefProviderResponseID,
		Value:    "provider_prev_id",
	}
	record := makeRecord("resp_prev", testTarget(), prevReq, prevResp, nativeRef)
	spy.Put(context.Background(), testScope, record)

	turn := canonical.NewTurnRef("resp_prev")
	req := makeRequest("claude-3-sonnet", makeItems("turn2"), turn)

	got, native, err := Prepare(context.Background(), spy, testScope, testTargetDifferentPtr(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if native != nil {
		t.Errorf("expected nil native ref for different target, got %+v", native)
	}
	items := got.Items()
	if len(items) != 3 {
		t.Fatalf("expected 3 items (turn1 + assistant1 + turn2), got %d: %+v", len(items), items)
	}
	if items[0].Text != "turn1" {
		t.Errorf("expected item[0]=turn1, got %s", items[0].Text)
	}
	if items[1].Text != "assistant1" {
		t.Errorf("expected item[1]=assistant1, got %s", items[1].Text)
	}
	if items[2].Text != "turn2" {
		t.Errorf("expected item[2]=turn2, got %s", items[2].Text)
	}
}

func TestPrepareAllowsMultiItemCurrentTurnWithPreviousResponseID(t *testing.T) {
	spy := newSpyStore()
	prevReq := makeRequest("gpt-4o", makeItems("turn1"), canonical.TurnRef{})
	prevResp := makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1"))
	record := makeRecord("resp_prev", testTarget(), prevReq, prevResp, nil)
	spy.Put(context.Background(), testScope, record)

	currentItems := []canonical.CanonicalItem{
		canonical.NewTextItem(canonical.ItemAuthorUser, "turn2a"),
		canonical.NewTextItem(canonical.ItemAuthorUser, "turn2b"),
	}
	turn := canonical.NewTurnRef("resp_prev")
	req := makeRequest("gpt-4o", currentItems, turn)

	got, native, err := Prepare(context.Background(), spy, testScope, testTargetDifferentPtr(), req)
	if err != nil {
		t.Fatalf("unexpected error for current-turn input with previous_response_id")
	}
	if native != nil {
		t.Fatalf("expected nil native ref for different target, got %+v", native)
	}
	items := got.Items()
	if len(items) != 4 {
		t.Fatalf("expected 4 items (turn1 + assistant1 + turn2a + turn2b), got %d: %+v", len(items), items)
	}
	if items[0].Text != "turn1" || items[1].Text != "assistant1" || items[2].Text != "turn2a" || items[3].Text != "turn2b" {
		t.Fatalf("unexpected materialized items: %+v", items)
	}
}

func TestPrepareStoreNilWithPreviousRejects(t *testing.T) {
	turn := canonical.NewTurnRef("resp_prev")
	req := makeRequest("gpt-4o", makeItems("hello"), turn)

	_, _, err := Prepare(context.Background(), nil, testScope, testTargetPtr(), req)
	if err == nil {
		t.Fatal("expected error when store is nil and previous is present")
	}
}

func TestPrepareInheritsMissingFieldsFromPrevious(t *testing.T) {
	spy := newSpyStore()
	prevReq := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "gpt-4o",
		Instructions: "You are helpful",
		Items:        []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn1")},
	})
	prevResp := canonical.NewConversationOutput("resp_prev", "gpt-4o", []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "ok")}, "stop")
	record := makeRecord("resp_prev", testTarget(), prevReq, prevResp, nil)
	spy.Put(context.Background(), testScope, record)

	turn := canonical.NewTurnRef("resp_prev")
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn2")},
		Turn:  turn,
	})

	got, _, err := Prepare(context.Background(), spy, testScope, testTargetDifferentPtr(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model() != "gpt-4o" {
		t.Errorf("expected inherited model gpt-4o, got %s", got.Model())
	}
	if got.Instructions() != "You are helpful" {
		t.Errorf("expected inherited instructions, got %s", got.Instructions())
	}
}

func TestPreparePrefersCurrentFieldsOverPrevious(t *testing.T) {
	spy := newSpyStore()
	prevReq := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "gpt-4o",
		Instructions: "Old instr",
		Items:        []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn1")},
	})
	prevResp := canonical.NewConversationOutput("resp_prev", "gpt-4o", []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "ok")}, "stop")
	record := makeRecord("resp_prev", testTarget(), prevReq, prevResp, nil)
	spy.Put(context.Background(), testScope, record)

	turn := canonical.NewTurnRef("resp_prev")
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "claude-3-sonnet",
		Instructions: "New instr",
		Items:        []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn2")},
		Turn:         turn,
	})

	got, _, err := Prepare(context.Background(), spy, testScope, testTargetDifferentPtr(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model() != "claude-3-sonnet" {
		t.Errorf("expected current model, got %s", got.Model())
	}
	if got.Instructions() != "New instr" {
		t.Errorf("expected current instructions, got %s", got.Instructions())
	}
}

func TestMaterializeDoesNotAliasPreviousRecordItems(t *testing.T) {
	prevReq := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "gpt-4o",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn1")},
	})
	prevResp := canonical.NewConversationOutput("resp_prev", "gpt-4o", []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")}, "stop")
	previous := makeRecord("resp_prev", testTarget(), prevReq, prevResp, nil)
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn2")},
	})

	got := materialize(previous, current)
	gotItems := got.Items()
	gotItems[0].Text = "mutated"

	if previous.Request.Items()[0].Text != "turn1" {
		t.Fatalf("previous request item mutated through materialize result: %+v", previous.Request.Items())
	}
	if current.Items()[0].Text != "turn2" {
		t.Fatalf("current request item mutated through materialize result: %+v", current.Items())
	}
}

func TestMaterializeDoesNotAliasInheritedTools(t *testing.T) {
	prevReq := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "gpt-4o",
		Tools: []canonical.ToolDecl{
			canonical.NewFunctionToolDecl("search", "search", "search text", canonical.NewToolSchemaObject(`{"type":"object","properties":{"q":{"type":"string"}}}`)),
		},
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn1")},
	})
	prevResp := canonical.NewConversationOutput("resp_prev", "gpt-4o", []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1")}, "stop")
	previous := makeRecord("resp_prev", testTarget(), prevReq, prevResp, nil)
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn2")},
	})

	got := materialize(previous, current)
	gotTools := got.Tools()
	if len(gotTools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(gotTools))
	}
	if tool, ok := gotTools[0].(canonical.FunctionToolDecl); ok {
		tool.Description = "mutated"
		gotTools[0] = tool
	} else {
		t.Fatalf("expected FunctionToolDecl, got %T", gotTools[0])
	}

	prevTools := previous.Request.Tools()
	if len(prevTools) != 1 {
		t.Fatalf("expected previous tool slice to remain intact, got %d", len(prevTools))
	}
	if prevTools[0].ToolDescription() != "search text" {
		t.Fatalf("previous tool description mutated through materialize result: %q", prevTools[0].ToolDescription())
	}
}

func floatPtr(v float64) *float64 {
	return &v
}

func TestPrepareUnsafeNativeReplayReturnsFullRequestAndNilNative(t *testing.T) {
	spy := newSpyStore()
	prevReq := makeRequest("gpt-4o", makeItems("turn1"), canonical.TurnRef{})
	prevResp := makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1"))
	nativeRef := &NativeRef{
		ReplayID: "resp_prev",
		Target:   testTarget(),
		Kind:     NativeRefProviderResponseID,
		Value:    "provider_prev_id",
	}
	record := makeRecord("resp_prev", testTarget(), prevReq, prevResp, nativeRef)
	spy.Put(context.Background(), testScope, record)

	turn := canonical.NewTurnRef("resp_prev")
	req := makeRequest("gpt-4o", makeItems("turn2"), turn)

	got, native, err := Prepare(context.Background(), spy, testScope, nil, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if native != nil {
		t.Fatalf("expected nil native ref when native replay is disallowed, got %+v", native)
	}
	items := got.Items()
	if len(items) != 3 {
		t.Fatalf("expected 3 items (turn1 + assistant1 + turn2), got %d: %+v", len(items), items)
	}
	if items[0].Text != "turn1" || items[1].Text != "assistant1" || items[2].Text != "turn2" {
		t.Fatalf("unexpected materialized items: %+v", items)
	}
}

// --- CaptureRequest tests ---

func TestCaptureRequestWithNilNativeReturnsProviderRequest(t *testing.T) {
	spy := newSpyStore()
	req := makeRequest("gpt-4o", makeItems("hello"), canonical.TurnRef{})

	got, err := CaptureRequest(context.Background(), spy, testScope, nil, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spy.getCalled {
		t.Error("expected store.Get not to be called when native is nil")
	}
	if !got.Turn().IsZero() {
		t.Errorf("expected turn to be cleared, got %+v", got.Turn())
	}
	items := got.Items()
	if len(items) != 1 || items[0].Text != "hello" {
		t.Errorf("expected [hello], got %+v", items)
	}
}

func TestCaptureRequestWithNativeMaterializesFullRequest(t *testing.T) {
	spy := newSpyStore()
	prevItems := makeItems("turn1")
	prevReq := makeRequest("gpt-4o", prevItems, canonical.TurnRef{})
	prevResp := makeResponse(canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1"))
	record := makeRecord("resp_prev", testTarget(), prevReq, prevResp, nil)
	spy.Put(context.Background(), testScope, record)

	native := &NativeRef{ReplayID: "resp_prev", Target: testTarget(), Kind: NativeRefProviderResponseID, Value: "provider_prev"}
	deltaReq := makeRequest("gpt-4o", makeItems("turn2"), canonical.TurnRef{})

	got, err := CaptureRequest(context.Background(), spy, testScope, native, deltaReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := got.Items()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d: %+v", len(items), items)
	}
	if items[0].Text != "turn1" {
		t.Errorf("expected item[0]=turn1, got %s", items[0].Text)
	}
	if items[1].Text != "assistant1" {
		t.Errorf("expected item[1]=assistant1, got %s", items[1].Text)
	}
	if items[2].Text != "turn2" {
		t.Errorf("expected item[2]=turn2, got %s", items[2].Text)
	}
}

func TestCaptureRequestWithNativeMissingParentErrors(t *testing.T) {
	spy := newSpyStore()
	native := &NativeRef{ReplayID: "resp_missing", Target: testTarget(), Kind: NativeRefProviderResponseID, Value: "provider_prev"}
	deltaReq := makeRequest("gpt-4o", makeItems("turn2"), canonical.TurnRef{})

	_, err := CaptureRequest(context.Background(), spy, testScope, native, deltaReq)
	if err == nil {
		t.Fatal("expected error when native parent is missing")
	}
}

func TestCaptureRequestInheritsMissingFieldsFromPrevious(t *testing.T) {
	spy := newSpyStore()
	prevReq := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "gpt-4o",
		Instructions: "You are helpful",
		Items:        []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn1")},
	})
	prevResp := canonical.NewConversationOutput("resp_prev", "gpt-4o", []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "ok")}, "stop")
	record := makeRecord("resp_prev", testTarget(), prevReq, prevResp, nil)
	spy.Put(context.Background(), testScope, record)

	native := &NativeRef{ReplayID: "resp_prev", Target: testTarget(), Kind: NativeRefProviderResponseID, Value: "provider_prev"}
	deltaReq := canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn2")},
	})

	got, err := CaptureRequest(context.Background(), spy, testScope, native, deltaReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model() != "gpt-4o" {
		t.Errorf("expected inherited model gpt-4o, got %s", got.Model())
	}
	if got.Instructions() != "You are helpful" {
		t.Errorf("expected inherited instructions, got %s", got.Instructions())
	}
}

// --- MemoryStore roundtrip test ---

func TestMemoryStoreGetPutRoundtrip(t *testing.T) {
	store := newMemoryStore()
	record := makeRecord("resp_1", testTarget(), makeRequest("m", nil, canonical.TurnRef{}), makeResponse(), nil)

	if err := store.Put(context.Background(), testScope, record); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	got, found, err := store.Get(context.Background(), testScope, "resp_1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !found {
		t.Fatal("expected record to be found")
	}
	if got.ID != "resp_1" {
		t.Errorf("expected id resp_1, got %s", got.ID)
	}

	_, found, err = store.Get(context.Background(), testScope, "resp_missing")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if found {
		t.Error("expected missing record not to be found")
	}
}
