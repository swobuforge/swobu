package effect

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/turnstate"
)

type recordingObservationStore struct {
	records []observation.ObservationRecord
}

func (s *recordingObservationStore) Put(_ context.Context, obs observation.ObservationRecord) error {
	s.records = append(s.records, obs)
	return nil
}

func (s *recordingObservationStore) Query(context.Context, observation.ObservationQuerySpec) ([]observation.ObservationRecord, error) {
	return nil, nil
}

type recordingTurnStateStore struct {
	records []turnstate.TurnStateKey
	values  [][]byte
}

func (s *recordingTurnStateStore) Put(_ context.Context, key turnstate.TurnStateKey, value []byte) error {
	s.records = append(s.records, key)
	s.values = append(s.values, append([]byte(nil), value...))
	return nil
}

func (s *recordingTurnStateStore) Get(context.Context, turnstate.TurnStateKey) ([]byte, bool, error) {
	return nil, false, nil
}

func TestStoreBackedSink_CommitPersistsCompatibilityAndTurnState(t *testing.T) {
	store := &recordingObservationStore{}
	turnStore := &recordingTurnStateStore{}
	sink := StoreBackedSink{Observations: store, TurnState: turnStore}

	err := sink.Commit(context.Background(), "ex-1", []Effect{
		CompatibilityEffect{
			Feature: compat.ToolCallID,
			Outcome: compat.Approx,
			Subject: compat.Subject("wire:/input/0/call_id"),
		},
		TurnStateEffect{
			Op:    TurnStateReplay,
			Key:   "turn.request.raw",
			Value: []byte("raw-bytes"),
		},
	})
	if err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	if len(store.records) != 1 {
		t.Fatalf("records len=%d want=1", len(store.records))
	}

	first := store.records[0]
	if first.Code != string(compat.ToolCallID) {
		t.Fatalf("first record code = %q, want %q", first.Code, compat.ToolCallID)
	}
	if first.Reason != "approx wire:/input/0/call_id" {
		t.Fatalf("first record reason = %q, want approx wire:/input/0/call_id", first.Reason)
	}
	if first.ObservedAt == 0 {
		t.Fatal("first record observed_at must be stamped")
	}
	if len(turnStore.records) != 1 {
		t.Fatalf("turn state len=%d want=1", len(turnStore.records))
	}
	if got := turnStore.records[0].Subject; got != "turn.request.raw" {
		t.Fatalf("turn state subject = %q, want turn.request.raw", got)
	}
	if got := turnStore.records[0].Kind; got != turnstate.TurnStateKind(TurnStateReplay) {
		t.Fatalf("turn state kind = %q, want %q", got, TurnStateReplay)
	}
	if got := string(turnStore.values[0]); got != "raw-bytes" {
		t.Fatalf("turn state value = %q, want raw-bytes", got)
	}
}
