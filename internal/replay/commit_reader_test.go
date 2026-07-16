package replay

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestCommitReader_StreamsNonTerminalBeforeCapture(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventTextDelta, EnvID: "r1", Payload: canonical.TextDeltaPayload{Text: "hello"}},
		{Kind: canonical.EventUsage, EnvID: "r1", Payload: canonical.UsagePayload{Usage: func() canonical.TokenUsage {
			u, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: intPtr(3), OutputTokens: intPtr(2)})
			return u
		}()}},
		{Kind: canonical.EventFinish, EnvID: "r1", Payload: canonical.FinishPayload{Reason: "completed"}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	store := newMemoryStore()
	config := TerminalCommitConfig{
		Scope:      Scope{Namespace: "ex1", CallerKey: "local"},
		ExchangeID: "ex1",
		ResponseID: "swobu_resp_1",
		Store:      store,
		CaptureRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
	}
	cr := NewCommitReader(events, config)
	ctx := context.Background()

	// Non-terminal events should be returned eagerly.
	for i := 0; i < 4; i++ {
		ev, err := cr.Next(ctx)
		if err != nil {
			t.Fatalf("event %d: unexpected error: %v", i, err)
		}
		if ev.Kind == canonical.EventEnvelopeEnd {
			t.Fatalf("event %d: got envelope.end before commit, want non-terminal", i)
		}
		if i == 0 {
			if ev.Meta.NativeID != "" {
				t.Fatalf("response start native id = %q, want provider native id to remain empty on the replay rewrite seam", ev.Meta.NativeID)
			}
			if ev.Meta.ResultID != string(config.ResponseID) {
				t.Fatalf("response start result id = %q, want %q", ev.Meta.ResultID, config.ResponseID)
			}
		}
	}

	// Terminal event should be withheld until commit completes.
	ev, err := cr.Next(ctx)
	if err != nil {
		t.Fatalf("terminal event: unexpected error: %v", err)
	}
	if ev.Kind != canonical.EventEnvelopeEnd {
		t.Fatalf("terminal event kind = %v, want envelope.end", ev.Kind)
	}

	// Store should have the record.
	rec, ok, err := store.Get(ctx, config.Scope, ReplayIDFromResponseID(config.ResponseID))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("record not found after commit")
	}
	if rec.ID != ReplayIDFromResponseID(config.ResponseID) {
		t.Fatalf("record ID = %q, want %q", rec.ID, ReplayIDFromResponseID(config.ResponseID))
	}
	if rec.Response.ResultID() != "swobu_resp_1" {
		t.Fatalf("response ID = %q, want Swobu ID", rec.Response.ResultID())
	}
}

func TestCommitReader_WithNativeReplay_MaterializesFullStoredRequest(t *testing.T) {
	ctx := context.Background()
	scope := Scope{Namespace: "ex_native", CallerKey: "local"}
	store := newMemoryStore()
	prevRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn1")},
	})
	prevResponse := canonical.NewConversationOutput("resp_prev", "m", []canonical.CanonicalItem{
		canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1"),
	}, "stop")
	native := &NativeRef{
		ReplayID: "resp_prev",
		Target:   testTarget(),
		Kind:     NativeRefProviderResponseID,
		Value:    "provider_prev",
	}
	if err := store.Put(ctx, scope, Record{
		ID:       ReplayIDFromResponseID("resp_prev"),
		Scope:    scope,
		Request:  prevRequest,
		Response: prevResponse,
		Native:   native,
	}); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventMetadata, EnvID: "r1", Payload: canonical.MetadataPayload{Values: map[string]string{"result_id": "provider_resp_1", "model": "m"}}},
		{Kind: canonical.EventTextDelta, EnvID: "r1", Payload: canonical.TextDeltaPayload{Text: "turn2"}},
		{Kind: canonical.EventUsage, EnvID: "r1", Payload: canonical.UsagePayload{Usage: func() canonical.TokenUsage {
			u, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: intPtr(3), OutputTokens: intPtr(2)})
			return u
		}()}},
		{Kind: canonical.EventFinish, EnvID: "r1", Payload: canonical.FinishPayload{Reason: "completed"}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	config := TerminalCommitConfig{
		Scope:        scope,
		ExchangeID:   "ex_native",
		ResponseID:   "swobu_resp_native",
		Store:        store,
		NativeReplay: native,
		CaptureRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn2")},
		}),
	}
	cr := NewCommitReader(events, config)

	for {
		_, err := cr.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
	}

	rec, ok, err := store.Get(ctx, scope, ReplayIDFromResponseID(config.ResponseID))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("record missing")
	}
	items := rec.Request.Items()
	if len(items) != 3 {
		t.Fatalf("stored request items len = %d, want 3", len(items))
	}
	if items[0].Text != "turn1" || items[1].Text != "assistant1" || items[2].Text != "turn2" {
		t.Fatalf("stored request items = %+v, want full semantic history", items)
	}
}

func TestCommitReader_CommitsBeforeTerminalSuccess(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	store := newMemoryStore()
	config := TerminalCommitConfig{
		Scope:      Scope{Namespace: "ex2", CallerKey: "local"},
		ExchangeID: "ex2",
		ResponseID: "swobu_resp_2",
		Store:      store,
		CaptureRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
	}
	cr := NewCommitReader(events, config)
	ctx := context.Background()

	ev, err := cr.Next(ctx)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if ev.Kind != canonical.EventEnvelopeStart {
		t.Fatalf("first kind = %v, want envelope.start", ev.Kind)
	}

	// This Next triggers commit.
	ev, err = cr.Next(ctx)
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	if ev.Kind != canonical.EventEnvelopeEnd {
		t.Fatalf("terminal kind = %v, want envelope.end", ev.Kind)
	}

	rec, ok, err := store.Get(ctx, config.Scope, ReplayIDFromResponseID(config.ResponseID))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("record missing")
	}
	if rec.Response.ResultID() != "swobu_resp_2" {
		t.Fatalf("response ID = %q, want swobu_resp_2", rec.Response.ResultID())
	}
	if rec.ExpiresAt == nil {
		t.Fatal("expected replay record expiry to be set")
	}
	if !rec.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("replay record expiry = %v, want future time", rec.ExpiresAt)
	}
}

func TestCommitReader_EmptyResponseIDFailsTerminalCommit(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	store := newSpyStore()
	config := TerminalCommitConfig{
		Scope:      Scope{Namespace: "ex_empty", CallerKey: "local"},
		ExchangeID: "ex_empty",
		ResponseID: "",
		Store:      store,
		CaptureRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
	}
	cr := NewCommitReader(events, config)
	ctx := context.Background()

	_, err := cr.Next(ctx)
	if err != nil {
		t.Fatalf("first event: %v", err)
	}
	ev, err := cr.Next(ctx)
	if err != nil {
		t.Fatalf("terminal event: %v", err)
	}
	if ev.Kind != canonical.EventError {
		t.Fatalf("terminal event kind = %v, want error", ev.Kind)
	}
	if cr.CommitError() == nil {
		t.Fatal("expected commit error to be recorded")
	}
	if len(store.calls) != 0 {
		t.Fatalf("store calls = %v, want no commit attempts", store.calls)
	}

	endEv, err := cr.Next(ctx)
	if err != nil {
		t.Fatalf("synthetic end: %v", err)
	}
	if endEv.Kind != canonical.EventEnvelopeEnd {
		t.Fatalf("expected synthetic envelope.end, got %v", endEv.Kind)
	}
}

func TestCommitReader_CaptureFailureEmitsTerminalFailure(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	store := &failingStore{}
	config := TerminalCommitConfig{
		Scope:      Scope{Namespace: "ex3", CallerKey: "local"},
		ExchangeID: "ex3",
		ResponseID: "swobu_resp_3",
		Store:      store,
		CaptureRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
	}
	cr := NewCommitReader(events, config)
	ctx := context.Background()

	_, _ = cr.Next(ctx) // envelope.start

	ev, err := cr.Next(ctx) // triggers commit, fails
	if err != nil {
		t.Fatalf("unexpected error after failed commit: %v", err)
	}
	if ev.Kind != canonical.EventError {
		t.Fatalf("commit-failure event kind = %v, want error", ev.Kind)
	}
	payload, ok := ev.Payload.(canonical.ErrorPayload)
	if !ok {
		t.Fatalf("payload type = %T, want ErrorPayload", ev.Payload)
	}
	if payload.Code != "replay_capture_failed" {
		t.Fatalf("error code = %q, want replay_capture_failed", payload.Code)
	}
	if payload.Message != "response could not be captured for replay" {
		t.Fatalf("error message = %q, want generic replay capture message", payload.Message)
	}

	// The commit reader injects a synthetic EnvelopeEnd after the error
	// so downstream encoders that depend on terminal envelope shape
	// (e.g. buffered projection) do not hang.
	endEv, err := cr.Next(ctx)
	if err != nil {
		t.Fatalf("synthetic end: %v", err)
	}
	if endEv.Kind != canonical.EventEnvelopeEnd {
		t.Fatalf("expected synthetic envelope.end, got %v", endEv.Kind)
	}

	// EOF after synthetic end.
	_, err = cr.Next(ctx)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("after synthetic end: err = %v, want EOF", err)
	}
}

func TestCommitReader_EOFBeforeTerminalSuccessEmitsFailure(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventTextDelta, EnvID: "r1", Payload: canonical.TextDeltaPayload{Text: "hello"}},
	})
	store := newSpyStore()
	config := TerminalCommitConfig{
		Scope:      Scope{Namespace: "ex_eof", CallerKey: "local"},
		ExchangeID: "ex_eof",
		ResponseID: "swobu_resp_eof",
		Store:      store,
		CaptureRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
	}
	cr := NewCommitReader(events, config)
	ctx := context.Background()

	ev, err := cr.Next(ctx)
	if err != nil {
		t.Fatalf("first event: %v", err)
	}
	if ev.Kind != canonical.EventEnvelopeStart {
		t.Fatalf("first event kind = %v, want envelope.start", ev.Kind)
	}

	ev, err = cr.Next(ctx)
	if err != nil {
		t.Fatalf("second event: %v", err)
	}
	if ev.Kind != canonical.EventTextDelta {
		t.Fatalf("second event kind = %v, want text.delta", ev.Kind)
	}

	ev, err = cr.Next(ctx)
	if err != nil {
		t.Fatalf("EOF synthetic failure event: %v", err)
	}
	if ev.Kind != canonical.EventError {
		t.Fatalf("EOF synthetic failure kind = %v, want error", ev.Kind)
	}
	payload, ok := ev.Payload.(canonical.ErrorPayload)
	if !ok {
		t.Fatalf("EOF synthetic failure payload type = %T, want ErrorPayload", ev.Payload)
	}
	if payload.Code != "provider_stream_incomplete" {
		t.Fatalf("EOF synthetic failure code = %q, want provider_stream_incomplete", payload.Code)
	}
	if payload.Message != "provider stream ended before completed" {
		t.Fatalf("EOF synthetic failure message = %q, want generic provider stream completion message", payload.Message)
	}
	if cr.CommitError() == nil {
		t.Fatal("expected commit error to be recorded on EOF")
	}
	if len(store.calls) != 0 {
		t.Fatalf("store calls = %v, want no commit attempts on EOF", store.calls)
	}

	ev, err = cr.Next(ctx)
	if err != nil {
		t.Fatalf("synthetic end: %v", err)
	}
	if ev.Kind != canonical.EventEnvelopeEnd {
		t.Fatalf("synthetic end kind = %v, want envelope.end", ev.Kind)
	}
	endPayload, ok := ev.Payload.(canonical.EnvelopeEndPayload)
	if !ok {
		t.Fatalf("synthetic end payload type = %T, want EnvelopeEndPayload", ev.Payload)
	}
	if endPayload.Status != canonical.EnvelopeStatusError {
		t.Fatalf("synthetic end status = %v, want error", endPayload.Status)
	}
}

func TestCommitReader_ProviderTerminalErrorPassesThroughWithoutReplayCommit(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusError}},
	})
	store := newSpyStore()
	config := TerminalCommitConfig{
		Scope:      Scope{Namespace: "ex_err_terminal", CallerKey: "local"},
		ExchangeID: "ex_err_terminal",
		ResponseID: "swobu_resp_err_terminal",
		Store:      store,
		CaptureRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
	}
	cr := NewCommitReader(events, config)
	ctx := context.Background()

	ev, err := cr.Next(ctx)
	if err != nil {
		t.Fatalf("first event: %v", err)
	}
	if ev.Kind != canonical.EventEnvelopeStart {
		t.Fatalf("first event kind = %v, want envelope.start", ev.Kind)
	}

	ev, err = cr.Next(ctx)
	if err != nil {
		t.Fatalf("terminal event: %v", err)
	}
	if ev.Kind != canonical.EventEnvelopeEnd {
		t.Fatalf("terminal event kind = %v, want envelope.end", ev.Kind)
	}
	payload, ok := ev.Payload.(canonical.EnvelopeEndPayload)
	if !ok {
		t.Fatalf("terminal payload type = %T, want EnvelopeEndPayload", ev.Payload)
	}
	if payload.Status != canonical.EnvelopeStatusError {
		t.Fatalf("terminal status = %v, want error", payload.Status)
	}
	if cr.CommitError() != nil {
		t.Fatalf("commit error = %v, want nil", cr.CommitError())
	}
	if len(store.calls) != 0 {
		t.Fatalf("store calls = %v, want no replay commit", store.calls)
	}

	_, err = cr.Next(ctx)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("after terminal error: err = %v, want EOF", err)
	}
}

func TestCommitReader_ProviderTerminalIncompletePassesThroughWithoutSyntheticIncomplete(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatus("incomplete")}},
	})
	store := newSpyStore()
	config := TerminalCommitConfig{
		Scope:      Scope{Namespace: "ex_incomplete_terminal", CallerKey: "local"},
		ExchangeID: "ex_incomplete_terminal",
		ResponseID: "swobu_resp_incomplete_terminal",
		Store:      store,
		CaptureRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
	}
	cr := NewCommitReader(events, config)
	ctx := context.Background()

	ev, err := cr.Next(ctx)
	if err != nil {
		t.Fatalf("first event: %v", err)
	}
	if ev.Kind != canonical.EventEnvelopeStart {
		t.Fatalf("first event kind = %v, want envelope.start", ev.Kind)
	}

	ev, err = cr.Next(ctx)
	if err != nil {
		t.Fatalf("terminal event: %v", err)
	}
	if ev.Kind != canonical.EventEnvelopeEnd {
		t.Fatalf("terminal event kind = %v, want envelope.end", ev.Kind)
	}
	payload, ok := ev.Payload.(canonical.EnvelopeEndPayload)
	if !ok {
		t.Fatalf("terminal payload type = %T, want EnvelopeEndPayload", ev.Payload)
	}
	if payload.Status != canonical.EnvelopeStatus("incomplete") {
		t.Fatalf("terminal status = %v, want incomplete", payload.Status)
	}
	if cr.CommitError() != nil {
		t.Fatalf("commit error = %v, want nil", cr.CommitError())
	}
	if len(store.calls) != 0 {
		t.Fatalf("store calls = %v, want no replay commit", store.calls)
	}

	_, err = cr.Next(ctx)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("after terminal incomplete: err = %v, want EOF", err)
	}
}

func TestCommitReader_UpstreamErrorAfterResponseStartEmitsTerminalFailure(t *testing.T) {
	ctx := context.Background()
	store := newSpyStore()
	config := TerminalCommitConfig{
		Scope:      Scope{Namespace: "ex_err", CallerKey: "local"},
		ExchangeID: "ex_err",
		ResponseID: "swobu_resp_err",
		Store:      store,
		CaptureRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
	}
	cr := NewCommitReader(&errorAfterStartReader{
		events: []canonical.Event{
			{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
			{Kind: canonical.EventTextDelta, EnvID: "r1", Payload: canonical.TextDeltaPayload{Text: "hello"}},
		},
		err: errors.New("decoder failed"),
	}, config)

	ev, err := cr.Next(ctx)
	if err != nil {
		t.Fatalf("first event: %v", err)
	}
	if ev.Kind != canonical.EventEnvelopeStart {
		t.Fatalf("first event kind = %v, want envelope.start", ev.Kind)
	}
	ev, err = cr.Next(ctx)
	if err != nil {
		t.Fatalf("second event: %v", err)
	}
	if ev.Kind != canonical.EventTextDelta {
		t.Fatalf("second event kind = %v, want text.delta", ev.Kind)
	}

	ev, err = cr.Next(ctx)
	if err != nil {
		t.Fatalf("terminal failure event: %v", err)
	}
	if ev.Kind != canonical.EventError {
		t.Fatalf("terminal failure kind = %v, want error", ev.Kind)
	}
	payload, ok := ev.Payload.(canonical.ErrorPayload)
	if !ok {
		t.Fatalf("payload type = %T, want ErrorPayload", ev.Payload)
	}
	if payload.Code != "provider_stream_decode_failed" {
		t.Fatalf("error code = %q, want provider_stream_decode_failed", payload.Code)
	}
	if payload.Message != "provider stream failed after response start" {
		t.Fatalf("error message = %q, want generic provider stream failure message", payload.Message)
	}
	if cr.CommitError() == nil {
		t.Fatal("expected commit error to be recorded")
	}

	ev, err = cr.Next(ctx)
	if err != nil {
		t.Fatalf("synthetic end: %v", err)
	}
	if ev.Kind != canonical.EventEnvelopeEnd {
		t.Fatalf("synthetic end kind = %v, want envelope.end", ev.Kind)
	}
	if len(store.calls) != 0 {
		t.Fatalf("store calls = %v, want no commit attempts on upstream error", store.calls)
	}
}

func TestCommitReader_NativeExtractor(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventMetadata, EnvID: "r1", Payload: canonical.MetadataPayload{Values: map[string]string{"result_id": "provider_native_1", "model": "m"}}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	store := newMemoryStore()
	config := TerminalCommitConfig{
		Scope:      Scope{Namespace: "ex4", CallerKey: "local"},
		ExchangeID: "ex4",
		ResponseID: "swobu_resp_4",
		Store:      store,
		CaptureRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
		NativeExtractor: func(providerResultID string, rid ID) *NativeRef {
			if providerResultID == "" {
				return nil
			}
			return &NativeRef{
				ReplayID: rid,
				Target:   testTarget(),
				Kind:     NativeRefProviderResponseID,
				Value:    providerResultID,
			}
		},
	}
	cr := NewCommitReader(events, config)
	ctx := context.Background()

	// Consume all events.
	for {
		_, err := cr.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
	}

	rec, ok, err := store.Get(ctx, config.Scope, ReplayIDFromResponseID(config.ResponseID))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("record not found")
	}
	if rec.Native == nil {
		t.Fatal("expected Native ref, got nil")
	}
	if rec.Native.ReplayID != ReplayIDFromResponseID(config.ResponseID) {
		t.Fatalf("native replay id = %q, want %q", rec.Native.ReplayID, ReplayIDFromResponseID(config.ResponseID))
	}
	if rec.Native.Target != testTarget() {
		t.Fatalf("native target = %+v, want %+v", rec.Native.Target, testTarget())
	}
	if rec.Native.Kind != NativeRefProviderResponseID {
		t.Fatalf("native kind = %q, want %q", rec.Native.Kind, NativeRefProviderResponseID)
	}
	if rec.Native.Value != "provider_native_1" {
		t.Fatalf("native ref = %q, want provider_native_1", rec.Native.Value)
	}
}

func TestCommitReader_DoesNotExposeNativeIDFromEnvelopeStart(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Meta: canonical.EventMetadataFields{NativeID: "provider_native_start"}, Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	store := newMemoryStore()
	config := TerminalCommitConfig{
		Scope:      Scope{Namespace: "ex5", CallerKey: "local"},
		ExchangeID: "ex5",
		ResponseID: "swobu_resp_5",
		Store:      store,
		CaptureRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
		NativeExtractor: func(providerResultID string, rid ID) *NativeRef {
			if providerResultID == "" {
				return nil
			}
			return &NativeRef{
				ReplayID: rid,
				Target:   testTarget(),
				Kind:     NativeRefProviderResponseID,
				Value:    providerResultID,
			}
		},
	}
	cr := NewCommitReader(events, config)
	ctx := context.Background()

	ev, err := cr.Next(ctx)
	if err != nil {
		t.Fatalf("first event: %v", err)
	}
	if ev.Kind != canonical.EventEnvelopeStart {
		t.Fatalf("first event kind = %v, want envelope.start", ev.Kind)
	}
	if ev.Meta.NativeID != "" {
		t.Fatalf("response start native id = %q, want empty after replay rewrite", ev.Meta.NativeID)
	}
	if ev.Meta.ResultID != string(config.ResponseID) {
		t.Fatalf("response start result id = %q, want %q", ev.Meta.ResultID, config.ResponseID)
	}

	for {
		_, err := cr.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
	}

	rec, ok, err := store.Get(ctx, config.Scope, ReplayIDFromResponseID(config.ResponseID))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("record not found")
	}
	if rec.Native == nil {
		t.Fatal("expected Native ref, got nil")
	}
	if rec.Native.ReplayID != ReplayIDFromResponseID(config.ResponseID) {
		t.Fatalf("native replay id = %q, want %q", rec.Native.ReplayID, ReplayIDFromResponseID(config.ResponseID))
	}
	if rec.Native.Target != testTarget() {
		t.Fatalf("native target = %+v, want %+v", rec.Native.Target, testTarget())
	}
	if rec.Native.Kind != NativeRefProviderResponseID {
		t.Fatalf("native kind = %q, want %q", rec.Native.Kind, NativeRefProviderResponseID)
	}
	if rec.Native.Value != "provider_native_start" {
		t.Fatalf("native ref = %q, want provider_native_start", rec.Native.Value)
	}
}

type failingStore struct{}

func (f *failingStore) Get(ctx context.Context, scope Scope, id ID) (Record, bool, error) {
	return Record{}, false, nil
}

func (f *failingStore) Put(ctx context.Context, scope Scope, record Record) error {
	return errors.New("store.Put failed")
}

func intPtr(n int) *int {
	return &n
}

var (
	_ Store = (*failingStore)(nil)
)

type errorAfterStartReader struct {
	events []canonical.Event
	err    error
	index  int
}

func (r *errorAfterStartReader) Next(context.Context) (canonical.Event, error) {
	if r.index < len(r.events) {
		ev := r.events[r.index]
		r.index++
		return ev, nil
	}
	return canonical.Event{}, r.err
}

func (r *errorAfterStartReader) Close(context.Context) error { return nil }

var (
	_ canonical.EventReader = (*errorAfterStartReader)(nil)
)
