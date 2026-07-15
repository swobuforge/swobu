package canonical

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type fakeContinuationStore struct {
	records  map[ContinuationID]ContinuationRecord
	putErr   error
	getErr   error
	chainErr error
	stored   []ContinuationRecord
}

func (s *fakeContinuationStore) Put(_ context.Context, rec ContinuationRecord) error {
	if s.putErr != nil {
		return s.putErr
	}
	if s.records == nil {
		s.records = map[ContinuationID]ContinuationRecord{}
	}
	s.records[rec.ID] = rec.Clone()
	s.stored = append(s.stored, rec.Clone())
	return nil
}

func (s *fakeContinuationStore) Get(_ context.Context, id ContinuationID) (ContinuationRecord, bool, error) {
	if s.getErr != nil {
		return ContinuationRecord{}, false, s.getErr
	}
	rec, ok := s.records[id]
	if !ok {
		return ContinuationRecord{}, false, nil
	}
	return rec.Clone(), true, nil
}

func (s *fakeContinuationStore) Chain(_ context.Context, id ContinuationID) ([]ContinuationRecord, error) {
	if s.chainErr != nil {
		return nil, s.chainErr
	}
	if s.records == nil {
		return nil, nil
	}
	var reversed []ContinuationRecord
	seen := map[ContinuationID]struct{}{}
	current := id
	for {
		if _, ok := seen[current]; ok {
			return nil, fmt.Errorf("continuation chain cycle detected for %q", current)
		}
		seen[current] = struct{}{}
		rec, ok := s.records[current]
		if !ok {
			break
		}
		reversed = append(reversed, rec.Clone())
		if rec.Parent == nil || rec.Parent.IsZero() {
			break
		}
		current = rec.Parent.Clone()
	}
	if len(reversed) == 0 {
		return nil, nil
	}
	chain := make([]ContinuationRecord, len(reversed))
	for i := range reversed {
		chain[len(reversed)-1-i] = reversed[i]
	}
	return chain, nil
}

func TestContinuationRuntime_PrepareRequest_MaterializesCanonicalHistoryForNonResponsesTargets(t *testing.T) {
	store := &fakeContinuationStore{
		records: map[ContinuationID]ContinuationRecord{
			NewContinuationID("resp_prev"): {
				ID: NewContinuationID("resp_prev"),
				RequestDelta: NewCanonicalRequest(RequestParams{
					Model: "m",
					Items: []CanonicalItem{NewTextItem(ItemAuthorUser, "hi")},
				}),
				Response: NewConversationOutput(
					"resp_prev",
					"m",
					[]OutputItem{NewTextOutputItem("text_0", "hello")},
					"completed",
				),
				Status: ContinuationStatusCompleted,
			},
		},
	}
	runtime := NewContinuationRuntime(store)

	request, err := runtime.PrepareRequest(context.Background(), NewContinuationNamespace("alpha"), protocolkind.ChatCompletions, NewCanonicalRequest(RequestParams{
		Model: "m",
		Turn:  NewTurnRef("resp_prev"),
		Items: []CanonicalItem{
			NewTextItem(ItemAuthorUser, "continue"),
		},
	}))
	if err != nil {
		t.Fatalf("PrepareRequest returned error: %v", err)
	}
	if got := request.Turn().IsZero(); !got {
		t.Fatal("Turn().IsZero() = false, want true after materialization")
	}
	items := request.Items()
	if len(items) != 3 {
		t.Fatalf("materialized item len = %d, want 3", len(items))
	}
	if got := items[0].Text; got != "hi" {
		t.Fatalf("materialized prefix[0] = %q, want %q", got, "hi")
	}
	if got := items[2].Text; got != "continue" {
		t.Fatalf("latest text = %q, want %q", got, "continue")
	}
}

func TestContinuationRuntime_PrepareRequest_InheritsMissingSemanticBandsFromChain(t *testing.T) {
	maxTokens := 64
	temperature := 0.2
	controls, err := NewGenerationControls(GenerationControlsParams{
		MaxOutputTokens: &maxTokens,
		Temperature:     &temperature,
		StopSequences:   []string{"DONE"},
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}
	outputFormat, err := NewOutputFormat(OutputFormatParams{
		Kind:        OutputFormatJSONSchema,
		Name:        "continuation_reply",
		Description: "structured continuation reply",
		Schema:      NewRawJSONObject(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"],"additionalProperties":false}`),
		Strict:      true,
	})
	if err != nil {
		t.Fatalf("NewOutputFormat returned error: %v", err)
	}
	store := &fakeContinuationStore{
		records: map[ContinuationID]ContinuationRecord{
			NewContinuationID("resp_prev"): {
				ID: NewContinuationID("resp_prev"),
				RequestDelta: NewCanonicalRequest(RequestParams{
					Model:        "provider-model",
					Instructions: "Use native tools for filesystem work.",
					Items: []CanonicalItem{
						NewTextItem(ItemAuthorUser, "previous-turn"),
					},
					Tools: []ToolDecl{
						NewFunctionToolDecl("tool_1", "exec_command", "run a command", NewToolSchemaObject(`{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}`)),
					},
					ToolPolicy:    NewToolPolicy(ToolPolicyRequired, nil),
					ToolCallBatch: NewToolCallBatchPolicy(ToolCallBatchAtMostOne),
					Controls:      controls,
					OutputFormat:  outputFormat,
				}),
				Response: NewConversationOutput(
					"resp_prev",
					"provider-model",
					[]OutputItem{NewTextOutputItem("text_0", "previous-response")},
					"completed",
				),
				Status: ContinuationStatusCompleted,
			},
		},
	}
	runtime := NewContinuationRuntime(store)

	currentMaxTokens := 16
	currentControls, err := NewGenerationControls(GenerationControlsParams{
		MaxOutputTokens: &currentMaxTokens,
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}
	request, err := runtime.PrepareRequest(context.Background(), NewContinuationNamespace("alpha"), protocolkind.ChatCompletions, NewCanonicalRequest(RequestParams{
		Model: "provider-model",
		Turn:  NewTurnRef("resp_prev"),
		Items: []CanonicalItem{
			NewTextItem(ItemAuthorUser, "continue"),
		},
		Controls: currentControls,
	}))
	if err != nil {
		t.Fatalf("PrepareRequest returned error: %v", err)
	}
	if got := request.Model(); got != "provider-model" {
		t.Fatalf("model = %q, want provider-model", got)
	}
	if got := request.Instructions(); got != "Use native tools for filesystem work." {
		t.Fatalf("instructions = %q, want inherited instructions", got)
	}
	if got := len(request.Tools()); got != 1 {
		t.Fatalf("tool count = %d, want 1", got)
	}
	if got := request.Tools()[0].ToolName(); got != "exec_command" {
		t.Fatalf("tool name = %q, want exec_command", got)
	}
	if got := request.ToolPolicy(); got.Mode != ToolPolicyRequired {
		t.Fatalf("tool policy mode = %q, want required", got.Mode)
	}
	if got := request.ToolCallBatch(); got.Mode != ToolCallBatchAtMostOne {
		t.Fatalf("tool call batch mode = %q, want at_most_one", got.Mode)
	}
	if got, ok := request.Controls().Limits.MaxOutputTokens.Value(); !ok || got != 16 {
		t.Fatalf("max_output_tokens = (%d, %v), want (16, true)", got, ok)
	}
	if got, ok := request.Controls().Sampling.Temperature.Value(); !ok || got != 0.2 {
		t.Fatalf("temperature = (%f, %v), want (0.2, true)", got, ok)
	}
	if stops := request.Controls().Limits.StopSequences; len(stops) != 1 || stops[0] != "DONE" {
		t.Fatalf("stop_sequences = %#v, want [DONE]", stops)
	}
	if gotFormat := request.OutputFormat(); gotFormat.Kind != OutputFormatJSONSchema || gotFormat.Name != "continuation_reply" || gotFormat.Strict != true {
		t.Fatalf("output format = %#v, want inherited structured format", gotFormat)
	}
	if got := request.Turn().IsZero(); !got {
		t.Fatal("Turn().IsZero() = false, want true after materialization")
	}
	items := request.Items()
	if len(items) != 3 {
		t.Fatalf("materialized item len = %d, want 3", len(items))
	}
	if got := items[0].Text; got != "previous-turn" {
		t.Fatalf("materialized prefix[0] = %q, want %q", got, "previous-turn")
	}
	if got := items[2].Text; got != "continue" {
		t.Fatalf("latest text = %q, want %q", got, "continue")
	}
}

func TestContinuationRuntime_PrepareRequest_PreservesNativeContinuationWhenSafe(t *testing.T) {
	store := &fakeContinuationStore{
		records: map[ContinuationID]ContinuationRecord{
			NewContinuationID("resp_prev"): {
				ID: NewContinuationID("resp_prev"),
				RequestDelta: NewCanonicalRequest(RequestParams{
					Model: "m",
					Items: []CanonicalItem{NewTextItem(ItemAuthorUser, "hi")},
				}),
				Response: NewConversationOutput(
					"resp_prev",
					"m",
					[]OutputItem{NewTextOutputItem("text_0", "hello")},
					"completed",
				),
				Status: ContinuationStatusCompleted,
			},
		},
	}
	runtime := NewContinuationRuntime(store)

	request, err := runtime.PrepareRequest(
		context.Background(),
		NewContinuationNamespace("alpha"),
		protocolkind.Responses,
		NewCanonicalRequest(RequestParams{
			Model: "m",
			Turn:  NewTurnRef("resp_prev"),
			Items: []CanonicalItem{
				NewTextItem(ItemAuthorUser, "hi"),
				NewTextItem(ItemAuthorAssistant, "hello"),
				NewTextItem(ItemAuthorUser, "continue"),
			},
		}),
	)
	if err != nil {
		t.Fatalf("PrepareRequest returned error: %v", err)
	}
	if got := request.Turn().IsZero(); got {
		t.Fatal("Turn().IsZero() = true, want false for native continuation")
	}
	if got := len(request.Items()); got != 3 {
		t.Fatalf("thread len = %d, want 3", got)
	}
	if got := request.Items()[2].Text; got != "continue" {
		t.Fatalf("latest text = %q, want %q", got, "continue")
	}
}

func TestContinuationRuntime_PrepareRequest_FailsClosedOnUnsafeNativeReplaySelection(t *testing.T) {
	store := &fakeContinuationStore{
		records: map[ContinuationID]ContinuationRecord{
			NewContinuationID("resp_prev"): {
				ID: NewContinuationID("resp_prev"),
				RequestDelta: NewCanonicalRequest(RequestParams{
					Model: "m",
					Items: []CanonicalItem{
						NewTextItem(ItemAuthorUser, "hi"),
						NewTextItem(ItemAuthorAssistant, "hello"),
					},
				}),
				Response: NewConversationOutput(
					"resp_prev",
					"m",
					[]OutputItem{NewTextOutputItem("text_0", "hello")},
					"completed",
				),
				Status: ContinuationStatusCompleted,
			},
		},
	}
	runtime := NewContinuationRuntime(store)

	_, err := runtime.PrepareRequest(
		context.Background(),
		NewContinuationNamespace("alpha"),
		protocolkind.Responses,
		NewCanonicalRequest(RequestParams{
			Model: "m",
			Turn:  NewTurnRef("resp_prev"),
			Items: []CanonicalItem{
				NewTextItem(ItemAuthorUser, "hi"),
				NewTextItem(ItemAuthorUser, "bye"),
			},
		}),
	)
	if err == nil {
		t.Fatal("PrepareRequest returned nil error, want unsafe replay rejection")
	}
	var unsafeReplayErr UnsafeNativeReplayError
	if !errors.As(err, &unsafeReplayErr) {
		t.Fatalf("PrepareRequest returned err=%v, want UnsafeNativeReplayError", err)
	}
}

func TestContinuationRuntime_PrepareRequest_FailsClosedOnUnknownContinuationID(t *testing.T) {
	runtime := NewContinuationRuntime(&fakeContinuationStore{})
	_, err := runtime.PrepareRequest(
		context.Background(),
		NewContinuationNamespace("alpha"),
		protocolkind.Responses,
		NewCanonicalRequest(RequestParams{
			Model: "m",
			Turn:  NewTurnRef("resp_missing"),
			Items: []CanonicalItem{NewTextItem(ItemAuthorUser, "continue")},
		}),
	)
	if err == nil || !strings.Contains(err.Error(), "could not be rehydrated") {
		t.Fatalf("PrepareRequest returned err=%v, want explicit rehydrate failure", err)
	}
}

func TestContinuationRuntime_WrapResponseEnvelope_PersistsCompletedContinuationRecord(t *testing.T) {
	store := &fakeContinuationStore{}
	runtime := NewContinuationRuntime(store)

	output := NewConversationOutput("resp_env", "m", []OutputItem{
		NewTextOutputItem("text_0", "done"),
	}, "completed")

	wrapped, err := runtime.WrapResponseEnvelope(
		context.Background(),
		NewContinuationNamespace("alpha"),
		NewCanonicalRequest(RequestParams{
			Model: "m",
			Turn:  NewTurnRef("resp_prev"),
			Items: []CanonicalItem{NewTextItem(ItemAuthorUser, "hi")},
		}),
		NewSliceEventReader(SynthesizeResponseEnvelopeEvents("ex_wrap_env", output.ResultID(), output.Model(), output.Items(), output.FinishReason(), output.Usage())),
	)
	if err != nil {
		t.Fatalf("WrapResponseEnvelope returned error: %v", err)
	}
	for {
		_, err := wrapped.Next(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("wrapped.Next returned error: %v", err)
		}
	}
	if len(store.stored) != 1 {
		t.Fatalf("stored records = %d, want 1", len(store.stored))
	}
	if got := store.stored[0].ID.String(); got != "resp_env" {
		t.Fatalf("stored record id = %q, want %q", got, "resp_env")
	}
	if got := store.stored[0].RequestDelta.Items(); len(got) != 1 || got[0].Text != "hi" {
		t.Fatalf("stored request delta = %+v, want current turn only", got)
	}
}

func TestContinuationRuntime_WrapResponseEnvelope_DoesNotPersistOnUnexpectedEOF(t *testing.T) {
	store := &fakeContinuationStore{}
	runtime := NewContinuationRuntime(store)

	envelope := NewSliceEventReader([]Event{
		{ExchangeID: "ex_bad", Seq: 1, Kind: EventEnvelopeStart, EnvID: "r1", Payload: EnvelopeStartPayload{Kind: EnvResponse}},
		{ExchangeID: "ex_bad", Seq: 2, Kind: EventEnvelopeStart, EnvID: "m1", ParentID: "r1", Payload: EnvelopeStartPayload{Kind: EnvMessage, Role: ItemAuthorAssistant}},
		{ExchangeID: "ex_bad", Seq: 3, Kind: EventTextDelta, EnvID: "m1", Payload: TextDeltaPayload{Text: "partial"}},
	})

	wrapped, err := runtime.WrapResponseEnvelope(
		context.Background(),
		NewContinuationNamespace("alpha"),
		NewCanonicalRequest(RequestParams{
			Model: "m",
			Turn:  NewTurnRef("resp_prev"),
			Items: []CanonicalItem{NewTextItem(ItemAuthorUser, "hi")},
		}),
		envelope,
	)
	if err != nil {
		t.Fatalf("WrapResponseEnvelope returned error: %v", err)
	}
	for {
		_, err := wrapped.Next(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("wrapped.Next returned error: %v", err)
		}
	}
	if len(store.stored) != 0 {
		t.Fatalf("stored records = %d, want 0", len(store.stored))
	}
}
