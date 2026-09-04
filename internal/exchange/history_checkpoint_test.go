package exchange

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestImplicitHistoryResumesOnlyUniqueCurrentHead(t *testing.T) {
	store := continuity.NewMemoryStore()
	history := testExchangeHistory(t, "previous")
	checkpoint := testThreadCheckpoint(t, "resp_previous", &history)
	if _, err := store.StartThread(context.Background(), "dev", checkpoint); err != nil {
		t.Fatal(err)
	}
	current := testCanonicalRequest("a")
	state := reducerTestState(t)
	state.input.rebasedRequest = &wire.RebasedRequest{Previous: history, Request: current}
	state.input.requestFingerprint = testHistoryRequest([]byte("current"))
	runner := reducerRuntime()
	runner.CheckpointStore = store
	started, err := reduce(context.Background(), state, exchangeStarted{}, runner)
	if err != nil {
		t.Fatal(err)
	}
	loaded := executeCommand(context.Background(), started.command)
	resolved, err := reduce(context.Background(), started.nextState, loaded, runner)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.nextState.draft == nil || resolved.nextState.threadID.IsZero() || resolved.nextState.expectedHead != "resp_previous" {
		t.Fatalf("resolved lineage = draft:%t thread-present:%t head:%q", resolved.nextState.draft != nil, !resolved.nextState.threadID.IsZero(), resolved.nextState.expectedHead)
	}
}

func TestExplicitCurrentHeadResumesWithoutImplicitHistoryDigest(t *testing.T) {
	store := continuity.NewMemoryStore()
	previous := testThreadCheckpoint(t, "resp_explicit_only", nil)
	if _, err := store.StartThread(context.Background(), "dev", previous); err != nil {
		t.Fatal(err)
	}
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify("a"),
		Items:            []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "turn two")},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_explicit_only"},
	})
	state := reducerTestState(t)
	state.input.request = current
	state.input.requestFingerprint = testHistoryRequest([]byte("explicit-only-turn-two"))
	state.phase = loadingCheckpointPhase{explicit: true, reference: "resp_explicit_only", scheme: testHistoryScheme}
	outcome, err := reduce(context.Background(), state, checkpointLoaded{record: previous, resolution: continuity.HistoryUniqueHead, current: true}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, failed := outcome.nextState.phase.(failedPhase); failed {
		t.Fatalf("explicit-only resume failed: %#v", outcome.nextState.phase)
	}
	if outcome.nextState.advance != nil {
		t.Fatal("explicit-only lineage incorrectly synthesized a partial history digest")
	}
	if outcome.nextState.threadID != previous.ThreadID || outcome.nextState.expectedHead != previous.ResponseID {
		t.Fatalf("explicit-only lineage lost CAS identity: thread-match=%t head=%q", outcome.nextState.threadID == previous.ThreadID, outcome.nextState.expectedHead)
	}
}

func TestImplicitHistoryMissStartsFromFullVisibleRequestWithNewThread(t *testing.T) {
	history := testExchangeHistory(t, "missing")
	full := testCanonicalRequest("a")
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("a"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "current")},
	})
	state := reducerTestState(t)
	state.input.request = full
	state.input.rebasedRequest = &wire.RebasedRequest{Previous: history, Request: current}
	state.phase = loadingCheckpointPhase{history: history}
	outcome, err := reduce(context.Background(), state, checkpointLoaded{resolution: continuity.HistoryNotFound}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.nextState.draft == nil || outcome.nextState.draft.Current().Model() != full.Model() {
		t.Fatalf("fallback draft = %#v, want full visible request", outcome.nextState.draft)
	}
	if outcome.nextState.threadID.IsZero() || outcome.nextState.expectedHead != "" {
		t.Fatalf("fallback thread state: thread-present=%t head=%q", !outcome.nextState.threadID.IsZero(), outcome.nextState.expectedHead)
	}
	if outcome.nextState.advance == nil || outcome.nextState.advance.Previous == nil || *outcome.nextState.advance.Previous != history {
		t.Fatalf("fallback advance = %#v, want visible predecessor seed", outcome.nextState.advance)
	}
}

func TestImplicitHistoryAmbiguityStartsNewThreadWithoutHiddenHead(t *testing.T) {
	history := testExchangeHistory(t, "ambiguous")
	state := reducerTestState(t)
	state.input.rebasedRequest = &wire.RebasedRequest{Previous: history, Request: testCanonicalRequest("a")}
	state.phase = loadingCheckpointPhase{history: history}
	outcome, err := reduce(context.Background(), state, checkpointLoaded{resolution: continuity.HistoryAmbiguous}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.nextState.draft == nil || outcome.nextState.threadID.IsZero() || outcome.nextState.expectedHead != "" {
		t.Fatalf("ambiguous fallback retained hidden lineage: draft=%t thread-present=%t head=%q", outcome.nextState.draft != nil, !outcome.nextState.threadID.IsZero(), outcome.nextState.expectedHead)
	}
}

func TestNewExplicitThreadPreservesVisibleHistoryAcrossCommitAndResume(t *testing.T) {
	ctx := context.Background()
	store := continuity.NewMemoryStore()
	threadID := testThreadID("imported-conversation")
	previousHistory := testExchangeHistory(t, "visible-turn-one")
	currentRequest := testCanonicalRequest("a")
	requestFingerprint := testHistoryRequest([]byte("visible-turn-two-request"))

	state := reducerTestState(t)
	state.input.explicitThreadID = threadID
	state.input.request = currentRequest
	state.input.requestFingerprint = requestFingerprint
	state.input.rebasedRequest = &wire.RebasedRequest{Previous: previousHistory, Request: currentRequest}
	runner := reducerRuntime()
	runner.CheckpointStore = store

	started, err := reduce(ctx, state, exchangeStarted{}, runner)
	if err != nil {
		t.Fatal(err)
	}
	historyMiss := executeCommand(ctx, started.command)
	checking, err := reduce(ctx, started.nextState, historyMiss, runner)
	if err != nil {
		t.Fatal(err)
	}
	threadMiss := executeCommand(ctx, checking.command)
	ready, err := reduce(ctx, checking.nextState, threadMiss, runner)
	if err != nil {
		t.Fatal(err)
	}

	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: state.swobuResponseID}, "a", nil,
		canonical.Completed("stop"), canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	responseFingerprint := testHistoryResponse([]byte("visible-turn-two-response"))
	committer := checkpointCommitter{
		exchangeID: state.input.exchangeID, workspaceSlug: "dev", store: store,
		request: currentRequest, historyScheme: testHistoryScheme,
		advance: ready.nextState.advance, threadID: ready.nextState.threadID,
	}
	if err := committer.commitDocument(ctx, response, responseFingerprint); err != nil {
		t.Fatal(err)
	}
	wantHistory, err := historyfingerprint.Advance(&previousHistory, requestFingerprint, *responseFingerprint)
	if err != nil {
		t.Fatal(err)
	}

	next := reducerTestState(t)
	next.input.explicitThreadID = threadID
	next.input.rebasedRequest = &wire.RebasedRequest{Previous: wantHistory, Request: testCanonicalRequest("a")}
	nextStarted, err := reduce(ctx, next, exchangeStarted{}, runner)
	if err != nil {
		t.Fatal(err)
	}
	nextLoaded := executeCommand(ctx, nextStarted.command)
	nextReady, err := reduce(ctx, nextStarted.nextState, nextLoaded, runner)
	if err != nil {
		t.Fatal(err)
	}
	if _, failed := nextReady.nextState.phase.(failedPhase); failed || nextReady.nextState.expectedHead != state.swobuResponseID {
		t.Fatalf("next full-history request did not resume explicit thread: phase=%T head=%q", nextReady.nextState.phase, nextReady.nextState.expectedHead)
	}
}

func TestExplicitCheckpointRejectsStaleHeadAndWrongCodecScheme(t *testing.T) {
	record := testThreadCheckpoint(t, "resp_previous", nil)
	cases := []struct {
		name    string
		phase   loadingCheckpointPhase
		current bool
	}{
		{name: "stale head", phase: loadingCheckpointPhase{explicit: true, reference: record.ResponseID, scheme: record.HistoryScheme}},
		{name: "wrong scheme", phase: loadingCheckpointPhase{explicit: true, reference: record.ResponseID, scheme: "messages/v1"}, current: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			state := reducerTestState(t)
			initialThreadID := state.threadID
			state.phase = test.phase
			outcome, err := reduce(context.Background(), state, checkpointLoaded{record: record, resolution: continuity.HistoryUniqueHead, current: test.current}, reducerRuntime())
			if err != nil {
				t.Fatal(err)
			}
			failed, ok := outcome.nextState.phase.(failedPhase)
			if !ok || canonical.TerminalErrorCode(failed.problem) != canonical.ErrorCodeBadRequest {
				t.Fatalf("phase = %#v, want BAD_REQUEST", outcome.nextState.phase)
			}
			if outcome.nextState.draft != nil || outcome.nextState.threadID != initialThreadID || outcome.nextState.expectedHead != "" {
				t.Fatal("rejected explicit checkpoint mutated resume state")
			}
		})
	}
}

func TestProviderCallCarriesCompleteRequestPlusExactPreviousHistory(t *testing.T) {
	target := provider.NewTargetSnapshot("target-a", "openai", "https://api.openai.test", "cred", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	target.TargetVersion = 3
	target.Model = "a"
	previousRequest := testCanonicalRequest("a")
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{
		SwobuID:   "resp_previous",
		Responses: &canonical.ResponsesContinuation{ProviderResponseID: "provider_previous", TargetID: target.TargetID, TargetVersion: target.TargetVersion},
	}, "a", []canonical.CanonicalItem{testMessage(canonical.MessageRoleAssistant, "answer")}, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	current := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("a"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "current")},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_previous"},
	})
	resolved, err := continuity.Resume(current, continuity.Checkpoint{Request: previousRequest, Response: response})
	if err != nil {
		t.Fatal(err)
	}
	previous, ok := resolved.PreviousHistory(target.TargetID, target.TargetVersion)
	if !ok {
		t.Fatal("matching target did not expose previous history")
	}
	request := provider.Request{Canonical: resolved.Request(), PreviousHistory: &previous}
	if len(request.Canonical.Items()) != 3 || request.PreviousHistory.OmitStart != 0 || request.PreviousHistory.OmitEnd != 2 || request.PreviousHistory.Response.Responses == nil {
		t.Fatalf("provider request = %#v", request)
	}
}

func testExchangeHistory(t *testing.T, material string) historyfingerprint.History {
	return testExchangeHistoryForScheme(t, "responses/v1", material)
}

func testExchangeHistoryForScheme(t *testing.T, scheme historyfingerprint.Scheme, material string) historyfingerprint.History {
	t.Helper()
	request, err := historyfingerprint.FingerprintRequest(scheme, []byte("request:"+material))
	if err != nil {
		t.Fatal(err)
	}
	response, err := historyfingerprint.FingerprintResponse(scheme, []byte("response:"+material))
	if err != nil {
		t.Fatal(err)
	}
	history, err := historyfingerprint.Advance(nil, request, response)
	if err != nil {
		t.Fatal(err)
	}
	return history
}

func testThreadCheckpoint(t *testing.T, id canonical.SwobuResponseID, history *historyfingerprint.History) continuity.Checkpoint {
	t.Helper()
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "a", nil, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	scheme := testHistoryScheme
	if history != nil {
		scheme = history.Scheme()
	}
	return continuity.Checkpoint{ResponseID: id, ThreadID: testThreadID(string(id)), HistoryScheme: scheme, History: history, Request: testCanonicalRequest("a"), Response: response}
}
