package exchange

import (
	"context"
	"fmt"
	"testing"

	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestImplicitHistoryUsesOnlyUniqueCheckpoint(t *testing.T) {
	store := continuity.NewMemoryStore()
	history := testExchangeHistory(t, "previous")
	checkpoint := testCheckpoint(t, "resp_previous", &history)
	if err := store.Put(context.Background(), "dev", continuity.Commit{Checkpoint: checkpoint}); err != nil {
		t.Fatal(err)
	}
	state := reducerTestState(t)
	state.input.rebasedRequest = &wire.RebasedRequest{Previous: history, Request: testCanonicalRequest("a")}
	state.input.requestFingerprint = testHistoryRequest([]byte("current"))
	runner := reducerRuntime()
	runner.CheckpointStore = store
	started, err := reduce(context.Background(), state, exchangeStarted{}, runner)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := reduce(context.Background(), started.nextState, executeCommand(context.Background(), started.command), runner)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.nextState.draft == nil || resolved.nextState.executionAffinity != checkpoint.ExecutionAffinity {
		t.Fatal("unique checkpoint was not selected")
	}
	second := testCheckpoint(t, "resp_other", &history)
	second.ExecutionAffinity = testExecutionAffinity("other")
	if err := store.Put(context.Background(), "dev", continuity.Commit{Checkpoint: second}); err != nil {
		t.Fatal(err)
	}
	started, err = reduce(context.Background(), state, exchangeStarted{}, runner)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = reduce(context.Background(), started.nextState, executeCommand(context.Background(), started.command), runner)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.nextState.previousRequest != nil {
		t.Fatal("ambiguous history selected checkpoint-private state")
	}
}

func TestExplicitHistoricalCheckpointIsCrossCodecAndAffinityIndependent(t *testing.T) {
	history := testExchangeHistoryForScheme(t, "responses/v1", "previous")
	checkpoint := testCheckpoint(t, "resp_previous", &history)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("a"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "next")}, PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"}})
	state := reducerTestState(t)
	state.input.request = request
	state.input.requestFingerprint = testHistoryRequestForScheme(t, "messages/v1", []byte("next"))
	state.input.explicitExecutionAffinity = testExecutionAffinity("override")
	state.phase = loadingCheckpointPhase{explicit: true, reference: "resp_previous"}
	outcome, err := reduce(context.Background(), state, checkpointLoaded{record: checkpoint, found: true}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, failed := outcome.nextState.phase.(failedPhase); failed {
		t.Fatalf("cross-codec explicit resume failed: %#v", outcome.nextState.phase)
	}
	if outcome.nextState.advance != nil {
		t.Fatal("cross-codec resume manufactured history composition")
	}
	if outcome.nextState.executionAffinity != state.input.explicitExecutionAffinity {
		t.Fatal("explicit affinity did not override checkpoint affinity")
	}
}

func TestCheckpointCommitterAllowsSiblingResponses(t *testing.T) {
	store := continuity.NewMemoryStore()
	affinity := testExecutionAffinity("siblings")
	request := testCanonicalRequest("a")
	for _, id := range []canonical.SwobuResponseID{"resp_a", "resp_b"} {
		response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "a", nil, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
		if err != nil {
			t.Fatal(err)
		}
		committer := checkpointCommitter{exchangeID: string(id), workspaceSlug: "dev", store: store, request: request, executionAffinity: affinity}
		if err := committer.commitDocument(context.Background(), response, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []canonical.SwobuResponseID{"resp_a", "resp_b"} {
		if _, found, err := store.Get(context.Background(), "dev", id); err != nil || !found {
			t.Fatalf("sibling %s=(%t,%v)", id, found, err)
		}
	}
}

func TestTwoExchangesResolveOneCheckpointBeforeSiblingCommits(t *testing.T) {
	ctx := context.Background()
	store := continuity.NewMemoryStore()
	baseHistory := testExchangeHistory(t, "base")
	base := testCheckpoint(t, "resp_base", &baseHistory)
	if err := store.Put(ctx, "dev", continuity.Commit{Checkpoint: base}); err != nil {
		t.Fatal(err)
	}
	runner := reducerRuntime()
	runner.CheckpointStore = store
	resolved := make([]exchangeState, 2)
	for index := range resolved {
		request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("a"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "child")}, PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_base"}})
		state := reducerTestState(t)
		state.input.exchangeID = string(rune('a' + index))
		state.input.request = request
		state.input.requestFingerprint = testHistoryRequest([]byte{byte(index + 1)})
		started, err := reduce(ctx, state, exchangeStarted{}, runner)
		if err != nil {
			t.Fatal(err)
		}
		ready, err := reduce(ctx, started.nextState, executeCommand(ctx, started.command), runner)
		if err != nil {
			t.Fatal(err)
		}
		resolved[index] = ready.nextState
	}
	for index, state := range resolved {
		id := canonical.SwobuResponseID(fmt.Sprintf("resp_child_%d", index))
		response, _ := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "a", nil, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
		committer := checkpointCommitter{exchangeID: string(id), workspaceSlug: "dev", store: store, request: state.draft.Current(), executionAffinity: state.executionAffinity, storageReuse: state.storageReuse}
		if err := committer.commitDocument(ctx, response, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []canonical.SwobuResponseID{"resp_child_0", "resp_child_1"} {
		if _, found, err := store.Get(ctx, "dev", id); err != nil || !found {
			t.Fatalf("%s=(%t,%v)", id, found, err)
		}
	}
}

func testExchangeHistory(t *testing.T, material string) historyfingerprint.History {
	return testExchangeHistoryForScheme(t, "responses/v1", material)
}
func testExchangeHistoryForScheme(t *testing.T, scheme historyfingerprint.Scheme, material string) historyfingerprint.History {
	t.Helper()
	q, err := historyfingerprint.FingerprintRequest(scheme, []byte("q:"+material))
	if err != nil {
		t.Fatal(err)
	}
	a, err := historyfingerprint.FingerprintResponse(scheme, []byte("a:"+material))
	if err != nil {
		t.Fatal(err)
	}
	h, err := historyfingerprint.Advance(nil, q, a)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func testHistoryRequestForScheme(t *testing.T, scheme historyfingerprint.Scheme, material []byte) historyfingerprint.Request {
	t.Helper()
	value, err := historyfingerprint.FingerprintRequest(scheme, material)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func testCheckpoint(t *testing.T, id canonical.SwobuResponseID, history *historyfingerprint.History) continuity.Checkpoint {
	t.Helper()
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "a", nil, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	return continuity.Checkpoint{ExecutionAffinity: testExecutionAffinity(string(id)), History: history, Request: testCanonicalRequest("a"), Response: response}
}
