package canonical

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestIsPreviousResponseNotFoundBackendError(t *testing.T) {
	t.Run("exact typed responses error", func(t *testing.T) {
		err := NewBackendError("backend-a", 400, `{"error":{"message":"Previous response with id 'resp_missing' not found.","type":"invalid_request_error","param":"previous_response_id","code":"previous_response_not_found"}}`, "")
		if !IsPreviousResponseNotFoundBackendError(err) {
			t.Fatal("expected exact previous_response_not_found payload to match")
		}
	})

	t.Run("different param does not match", func(t *testing.T) {
		err := NewBackendError("backend-a", 400, `{"error":{"message":"bad","type":"invalid_request_error","param":"conversation","code":"previous_response_not_found"}}`, "")
		if IsPreviousResponseNotFoundBackendError(err) {
			t.Fatal("expected non-previous_response_id payload not to match")
		}
	})

	t.Run("plain text does not match", func(t *testing.T) {
		err := NewBackendError("backend-a", 400, "Previous response not found", "")
		if IsPreviousResponseNotFoundBackendError(err) {
			t.Fatal("expected heuristic text payload not to match")
		}
	})
}

type fakeContinuationStore struct {
	snapshot ContinuitySnapshot
	matches  map[ContinuationNamespace]ContinuationPrefixMatch
	ok       bool
	loadErr  error
	storeErr error
	stored   []ContinuitySnapshot
}

func (s *fakeContinuationStore) Load(context.Context, string) (ContinuitySnapshot, bool, error) {
	return s.snapshot.Clone(), s.ok, s.loadErr
}

func (s *fakeContinuationStore) MatchPrefix(_ context.Context, namespace ContinuationNamespace, _ []CanonicalItem) (ContinuationPrefixMatch, bool, error) {
	if s.loadErr != nil {
		return ContinuationPrefixMatch{}, false, s.loadErr
	}
	match, ok := s.matches[namespace]
	return ContinuationPrefixMatch{
		Snapshot:     match.Snapshot.Clone(),
		PrefixLength: match.PrefixLength,
	}, ok, nil
}

func (s *fakeContinuationStore) Store(_ context.Context, namespace ContinuationNamespace, snapshot ContinuitySnapshot) error {
	if s.storeErr != nil {
		return s.storeErr
	}
	if s.matches == nil {
		s.matches = map[ContinuationNamespace]ContinuationPrefixMatch{}
	}
	s.matches[namespace] = ContinuationPrefixMatch{
		Snapshot:     snapshot.Clone(),
		PrefixLength: len(snapshot.Thread),
	}
	s.stored = append(s.stored, snapshot.Clone())
	return nil
}

func TestContinuationRuntime_PrepareRequest_RehydratesCanonicalState(t *testing.T) {
	runtime := NewContinuationRuntime(&fakeContinuationStore{
		snapshot: NewContinuitySnapshot("resp_prev", "m", []CanonicalItem{
			NewTextItem(ItemAuthorUser, "hi"),
		}),
		ok: true,
	})

	request, err := runtime.PrepareRequest(context.Background(), NewContinuationNamespace("alpha"), "", NewGenerationRequest(GenerationRequestParams{
		Model:              "m",
		PreviousResponseID: "resp_prev",
		Thread: []CanonicalItem{
			NewTextItem(ItemAuthorUser, "continue"),
		},
	}))
	if err != nil {
		t.Fatalf("PrepareRequest returned error: %v", err)
	}
	typed, ok := request.(GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("request type = %T, want GenerationCanonicalRequest", request)
	}
	if got := len(typed.Thread()); got != 2 {
		t.Fatalf("thread len = %d, want 2", got)
	}
	if got := len(typed.LastTurn()); got != 1 {
		t.Fatalf("last turn len = %d, want 1", got)
	}
}

func TestContinuationRuntime_PrepareRequest_DerivesLastTurnForConversationRequestsOntoResponses(t *testing.T) {
	runtime := NewContinuationRuntime(&fakeContinuationStore{
		matches: map[ContinuationNamespace]ContinuationPrefixMatch{
			NewContinuationNamespace("alpha"): {
				Snapshot: NewContinuitySnapshot("resp_prev", "m", []CanonicalItem{
					NewTextItem(ItemAuthorUser, "hi"),
					NewTextItem(ItemAuthorAssistant, "hello"),
				}),
				PrefixLength: 2,
			},
		},
	})

	request, err := runtime.PrepareRequest(
		context.Background(),
		NewContinuationNamespace("alpha"),
		"responses",
		NewDialogRequest("m", []CanonicalItem{
			NewTextItem(ItemAuthorUser, "hi"),
			NewTextItem(ItemAuthorAssistant, "hello"),
			NewTextItem(ItemAuthorUser, "continue"),
		}),
	)
	if err != nil {
		t.Fatalf("PrepareRequest returned error: %v", err)
	}
	typed, ok := request.(GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("request type = %T, want GenerationCanonicalRequest", request)
	}
	if got := len(typed.Thread()); got != 3 {
		t.Fatalf("thread len = %d, want 3", got)
	}
	if got := len(typed.LastTurn()); got != 1 {
		t.Fatalf("last turn len = %d, want 1", got)
	}
	if got := typed.LastTurn()[0].Text; got != "continue" {
		t.Fatalf("last turn text = %q, want %q", got, "continue")
	}
}

func TestContinuationRuntime_PrepareRequest_DerivesLastTurnFromBestPrefixMatch(t *testing.T) {
	runtime := NewContinuationRuntime(&fakeContinuationStore{
		matches: map[ContinuationNamespace]ContinuationPrefixMatch{
			NewContinuationNamespace("alpha"): {
				Snapshot: NewContinuitySnapshot("resp_prev", "m", []CanonicalItem{
					NewTextItem(ItemAuthorUser, "shared"),
				}),
				PrefixLength: 1,
			},
		},
	})

	request, err := runtime.PrepareRequest(
		context.Background(),
		NewContinuationNamespace("alpha"),
		"responses",
		NewDialogRequest("m", []CanonicalItem{
			NewTextItem(ItemAuthorUser, "shared"),
			NewTextItem(ItemAuthorAssistant, "branch a"),
		}),
	)
	if err != nil {
		t.Fatalf("PrepareRequest returned error: %v", err)
	}
	typed, ok := request.(GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("request type = %T, want GenerationCanonicalRequest", request)
	}
	if got := len(typed.LastTurn()); got != 1 {
		t.Fatalf("last turn len = %d, want 1", got)
	}
	if got := typed.LastTurn()[0].Text; got != "branch a" {
		t.Fatalf("last turn text = %q, want %q", got, "branch a")
	}
}

func TestContinuationRuntime_WrapEnvelopeStream_PersistsOnCompletedResponseEnvelope(t *testing.T) {
	store := &fakeContinuationStore{}
	runtime := NewContinuationRuntime(store)

	output := NewConversationOutput("resp_env", "m", []OutputItem{
		NewTextOutputItem("text_0", "done"),
	}, "completed")
	envelope, err := EventReaderFromCanonicalOutput("ex_wrap_env", output)
	if err != nil {
		t.Fatalf("EventReaderFromCanonicalOutput returned error: %v", err)
	}

	wrapped, err := runtime.WrapEnvelopeStream(
		context.Background(),
		NewContinuationNamespace("alpha"),
		NewDialogRequest("m", []CanonicalItem{NewTextItem(ItemAuthorUser, "hi")}),
		envelope,
	)
	if err != nil {
		t.Fatalf("WrapEnvelopeStream returned error: %v", err)
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
		t.Fatalf("stored snapshots = %d, want 1", len(store.stored))
	}
}

func TestContinuationRuntime_WrapEnvelopeStream_DoesNotPersistOnUnexpectedEOF(t *testing.T) {
	store := &fakeContinuationStore{}
	runtime := NewContinuationRuntime(store)

	envelope := NewSliceEventReader([]Event{
		{ExchangeID: "ex_bad", Seq: 1, Kind: EventEnvelopeStart, EnvID: "r1", Payload: EnvelopeStartPayload{Kind: EnvResponse}},
		{ExchangeID: "ex_bad", Seq: 2, Kind: EventEnvelopeStart, EnvID: "m1", ParentID: "r1", Payload: EnvelopeStartPayload{Kind: EnvMessage, Role: ItemAuthorAssistant}},
		{ExchangeID: "ex_bad", Seq: 3, Kind: EventTextDelta, EnvID: "m1", Payload: TextDeltaPayload{Text: "partial"}},
	})

	wrapped, err := runtime.WrapEnvelopeStream(
		context.Background(),
		NewContinuationNamespace("alpha"),
		NewDialogRequest("m", []CanonicalItem{NewTextItem(ItemAuthorUser, "hi")}),
		envelope,
	)
	if err != nil {
		t.Fatalf("WrapEnvelopeStream returned error: %v", err)
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
		t.Fatalf("stored snapshots = %d, want 0", len(store.stored))
	}
}
