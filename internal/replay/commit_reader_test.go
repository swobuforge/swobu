package replay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestCommitReader_StreamsNonTerminalBeforeCapture(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventEnvelopeStart, EnvID: "m1", ParentID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant}},
		{Kind: canonical.EventTextDelta, EnvID: "m1", ParentID: "r1", Payload: canonical.TextDeltaPayload{Text: "hello"}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "m1", ParentID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvMessage, Status: canonical.EnvelopeStatusCompleted}},
		{Kind: canonical.EventUsage, EnvID: "r1", Payload: canonical.UsagePayload{Usage: func() canonical.TokenUsage {
			u, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: intPtr(3), OutputTokens: intPtr(2)})
			return u
		}()}},
		{Kind: canonical.EventFinish, EnvID: "r1", Payload: canonical.FinishPayload{Reason: "completed"}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	store := newMemoryStore()
	config := TerminalCommitConfig{
		WorkspaceSlug:   "ex1",
		ExchangeID:      "ex1",
		SwobuResponseID: "swobu_resp_1",
		Store:           store,
		SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
	}
	cr := NewCommitReader(events, config)
	ctx := context.Background()

	// Non-terminal events should be returned eagerly.
	for i := 0; i < 6; i++ {
		ev, err := cr.Next(ctx)
		if err != nil {
			t.Fatalf("event %d: unexpected error: %v", i, err)
		}
		if ev.Kind == canonical.EventEnvelopeEnd && ev.EnvID == "r1" {
			t.Fatalf("event %d: got response envelope.end before commit", i)
		}
		if i == 0 {
			if ev.Meta.NativeID != "" {
				t.Fatalf("response start native id = %q, want provider native id to remain empty on the replay rewrite seam", ev.Meta.NativeID)
			}
			payload := ev.Payload.(canonical.EnvelopeStartPayload)
			if payload.Response.SwobuID != config.SwobuResponseID {
				t.Fatalf("response start id = %q, want %q", payload.Response.SwobuID, config.SwobuResponseID)
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
	rec, ok, err := store.Get(ctx, config.WorkspaceSlug, config.SwobuResponseID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("record not found after commit")
	}
	if rec.Response.Response().SwobuID != config.SwobuResponseID {
		t.Fatalf("record response ID = %q, want %q", rec.Response.Response().SwobuID, config.SwobuResponseID)
	}
	if rec.Response.Response().SwobuID.String() != "swobu_resp_1" {
		t.Fatalf("response ID = %q, want Swobu ID", rec.Response.Response().SwobuID.String())
	}
}

func TestCommitReader_StoresPreparedFullSemanticRequest(t *testing.T) {
	ctx := context.Background()
	scope := "ex_native"
	store := newMemoryStore()
	prevRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn1")},
	})
	prevResponse := canonical.NewConversationOutput("resp_prev", "m", []canonical.CanonicalItem{
		canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant1"),
	}, "stop")
	semanticRequest := materialize(Record{Request: prevRequest, Response: prevResponse}, canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "turn2")},
	}))

	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{
			Kind: canonical.EnvResponse,
			Response: canonical.ResponseRef{Responses: &canonical.ResponsesNativeRef{
				ProviderResponseID: "provider_resp_1",
			}},
		}},
		{Kind: canonical.EventMetadata, EnvID: "r1", Payload: canonical.MetadataPayload{Values: map[string]string{"model": "m"}}},
		{Kind: canonical.EventEnvelopeStart, EnvID: "m1", ParentID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant}},
		{Kind: canonical.EventTextDelta, EnvID: "m1", ParentID: "r1", Payload: canonical.TextDeltaPayload{Text: "turn2"}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "m1", ParentID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvMessage, Status: canonical.EnvelopeStatusCompleted}},
		{Kind: canonical.EventUsage, EnvID: "r1", Payload: canonical.UsagePayload{Usage: func() canonical.TokenUsage {
			u, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: intPtr(3), OutputTokens: intPtr(2)})
			return u
		}()}},
		{Kind: canonical.EventFinish, EnvID: "r1", Payload: canonical.FinishPayload{Reason: "completed"}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	config := TerminalCommitConfig{
		WorkspaceSlug:   "ex_native",
		ExchangeID:      "ex_native",
		SwobuResponseID: "swobu_resp_native",
		TargetID:        "target-a",
		TargetVersion:   7,
		Store:           store,
		SemanticRequest: semanticRequest,
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

	rec, ok, err := store.Get(ctx, scope, config.SwobuResponseID)
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
	if canonicalItemText(items[0]) != "turn1" || canonicalItemText(items[1]) != "assistant1" || canonicalItemText(items[2]) != "turn2" {
		t.Fatalf("stored request items = %+v, want full semantic history", items)
	}
	response := rec.Response.Response()
	if response.Responses == nil || response.Responses.ProviderResponseID != "provider_resp_1" ||
		response.Responses.TargetID != "target-a" || response.Responses.TargetVersion != 7 {
		t.Fatalf("stored Responses refinement = %#v", response.Responses)
	}
}

func TestCommitReader_CommitsBeforeTerminalSuccess(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	store := newMemoryStore()
	config := TerminalCommitConfig{
		WorkspaceSlug:   "ex2",
		ExchangeID:      "ex2",
		SwobuResponseID: "swobu_resp_2",
		Store:           store,
		SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
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

	rec, ok, err := store.Get(ctx, config.WorkspaceSlug, config.SwobuResponseID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("record missing")
	}
	if rec.Response.Response().SwobuID.String() != "swobu_resp_2" {
		t.Fatalf("response ID = %q, want swobu_resp_2", rec.Response.Response().SwobuID.String())
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
		WorkspaceSlug:   "ex_empty",
		ExchangeID:      "ex_empty",
		SwobuResponseID: "",
		Store:           store,
		SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
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
		WorkspaceSlug:   "ex3",
		ExchangeID:      "ex3",
		SwobuResponseID: "swobu_resp_3",
		Store:           store,
		SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
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
		{Kind: canonical.EventEnvelopeStart, EnvID: "m1", ParentID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant}},
		{Kind: canonical.EventTextDelta, EnvID: "m1", ParentID: "r1", Payload: canonical.TextDeltaPayload{Text: "hello"}},
	})
	store := newSpyStore()
	config := TerminalCommitConfig{
		WorkspaceSlug:   "ex_eof",
		ExchangeID:      "ex_eof",
		SwobuResponseID: "swobu_resp_eof",
		Store:           store,
		SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
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
	if ev.Kind != canonical.EventEnvelopeStart {
		t.Fatalf("second event kind = %v, want message envelope.start", ev.Kind)
	}

	ev, err = cr.Next(ctx)
	if err != nil {
		t.Fatalf("third event: %v", err)
	}
	if ev.Kind != canonical.EventTextDelta {
		t.Fatalf("third event kind = %v, want text.delta", ev.Kind)
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
		WorkspaceSlug:   "ex_err_terminal",
		ExchangeID:      "ex_err_terminal",
		SwobuResponseID: "swobu_resp_err_terminal",
		Store:           store,
		SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
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
		WorkspaceSlug:   "ex_incomplete_terminal",
		ExchangeID:      "ex_incomplete_terminal",
		SwobuResponseID: "swobu_resp_incomplete_terminal",
		Store:           store,
		SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
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
		WorkspaceSlug:   "ex_err",
		ExchangeID:      "ex_err",
		SwobuResponseID: "swobu_resp_err",
		Store:           store,
		SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
	}
	cr := NewCommitReader(&errorAfterStartReader{
		events: []canonical.Event{
			{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
			{Kind: canonical.EventEnvelopeStart, EnvID: "m1", ParentID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant}},
			{Kind: canonical.EventTextDelta, EnvID: "m1", ParentID: "r1", Payload: canonical.TextDeltaPayload{Text: "hello"}},
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
	if ev.Kind != canonical.EventEnvelopeStart {
		t.Fatalf("second event kind = %v, want message envelope.start", ev.Kind)
	}

	ev, err = cr.Next(ctx)
	if err != nil {
		t.Fatalf("third event: %v", err)
	}
	if ev.Kind != canonical.EventTextDelta {
		t.Fatalf("third event kind = %v, want text.delta", ev.Kind)
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

func TestCommitReader_UpstreamErrorAfterResponseStartLogsDiagnosticContext(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{})))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})

	ctx := context.Background()
	cr := NewCommitReader(&errorAfterStartReader{
		events: []canonical.Event{
			{
				ExchangeID: "ex_log",
				Seq:        0,
				Kind:       canonical.EventEnvelopeStart,
				EnvID:      "r1",
				Payload:    canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse},
			},
			{
				ExchangeID: "ex_log",
				Seq:        1,
				Kind:       canonical.EventTextDelta,
				EnvID:      "r1:item:text_0",
				ParentID:   "r1",
				Payload:    canonical.TextDeltaPayload{Text: "hello"},
			},
		},
		err: errors.New("stream error: stream ID 81; CANCEL; received from peer"),
	}, TerminalCommitConfig{
		WorkspaceSlug:   "ex_log",
		ExchangeID:      "ex_log",
		SwobuResponseID: "swobu_resp_log",
		Store:           newSpyStore(),
		SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
	})

	for i := 0; i < 3; i++ {
		if _, err := cr.Next(ctx); err != nil {
			t.Fatalf("next %d: %v", i, err)
		}
	}

	logText := logs.String()
	wantFields := []string{
		"event=replay_terminal_failure",
		"code=provider_stream_decode_failed",
		"failure_origin=provider_stream_read_error",
		"response_started=true",
		"last_event_kind=text.delta",
		"last_event_seq=1",
		"last_env_id=r1:item:text_0",
		"recorded_event_count=2",
		"returned_event_count=2",
		"replace_last_event=false",
		"error_type=*errors.errorString",
		`error="stream error: stream ID 81; CANCEL; received from peer"`,
	}
	for _, field := range wantFields {
		if !strings.Contains(logText, field) {
			t.Fatalf("log missing %q in %s", field, logText)
		}
	}
}

func TestTerminalFailureOriginUsesGenericReplayCommitOrigin(t *testing.T) {
	if got := terminalFailureOrigin("replay_capture_failed"); got != "replay_commit" {
		t.Fatalf("origin = %q, want replay_commit", got)
	}
}

func TestCommitReader_StoresTypedResponsesRefinement(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Response: canonical.ResponseRef{Responses: &canonical.ResponsesNativeRef{ProviderResponseID: "provider_native_1"}}}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	store := newMemoryStore()
	target := testBackendTarget(t, "m")
	config := TerminalCommitConfig{
		WorkspaceSlug:   "ex4",
		ExchangeID:      "ex4",
		SwobuResponseID: "swobu_resp_4",
		Store:           store,
		SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
		TargetID:      target.TargetID,
		TargetVersion: target.TargetVersion,
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

	rec, ok, err := store.Get(ctx, config.WorkspaceSlug, config.SwobuResponseID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("record not found")
	}
	responses := rec.Response.Response().Responses
	if responses == nil || responses.TargetID != target.TargetID || responses.TargetVersion != target.TargetVersion || responses.ProviderResponseID != "provider_native_1" {
		t.Fatalf("Responses refinement = %#v", responses)
	}
}

func TestCommitReader_DoesNotExposeNativeIDFromEnvelopeStart(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{
		{Kind: canonical.EventEnvelopeStart, EnvID: "r1", Meta: canonical.EventMetadataFields{NativeID: "provider_native_start"}, Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Response: canonical.ResponseRef{Responses: &canonical.ResponsesNativeRef{ProviderResponseID: "provider_native_start"}}}},
		{Kind: canonical.EventEnvelopeEnd, EnvID: "r1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	})
	store := newMemoryStore()
	target := testBackendTarget(t, "m")
	config := TerminalCommitConfig{
		WorkspaceSlug:   "ex5",
		ExchangeID:      "ex5",
		SwobuResponseID: "swobu_resp_5",
		Store:           store,
		SemanticRequest: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
		TargetID:      target.TargetID,
		TargetVersion: target.TargetVersion,
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
	payload := ev.Payload.(canonical.EnvelopeStartPayload)
	if payload.Response.SwobuID != config.SwobuResponseID {
		t.Fatalf("response start id = %q, want %q", payload.Response.SwobuID, config.SwobuResponseID)
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

	rec, ok, err := store.Get(ctx, config.WorkspaceSlug, config.SwobuResponseID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("record not found")
	}
	responses := rec.Response.Response().Responses
	if responses == nil || responses.TargetID != target.TargetID || responses.TargetVersion != target.TargetVersion || responses.ProviderResponseID != "provider_native_start" {
		t.Fatalf("Responses refinement = %#v", responses)
	}
}

type failingStore struct{}

func (f *failingStore) Get(ctx context.Context, workspaceSlug string, id canonical.SwobuResponseID) (Record, bool, error) {
	return Record{}, false, nil
}

func (f *failingStore) Put(ctx context.Context, workspaceSlug string, record Record) error {
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
	_ canonical.ResponseStream = (*errorAfterStartReader)(nil)
)
