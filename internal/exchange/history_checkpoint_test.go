package exchange

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestImplicitHistoryResumesOnlyUniqueCurrentHead(t *testing.T) {
	store := session.NewMemoryStore()
	history := testExchangeHistory(t, "previous")
	checkpoint := testSessionCheckpoint(t, "resp_previous", &history)
	if _, err := store.StartSession(context.Background(), "dev", checkpoint); err != nil {
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
	if resolved.nextState.draft == nil || resolved.nextState.sessionID == "" || resolved.nextState.expectedHead != "resp_previous" {
		t.Fatalf("resolved lineage = draft:%t session:%q head:%q", resolved.nextState.draft != nil, resolved.nextState.sessionID, resolved.nextState.expectedHead)
	}
}

func TestExplicitCurrentHeadResumesWithoutImplicitHistoryDigest(t *testing.T) {
	store := session.NewMemoryStore()
	previous := testSessionCheckpoint(t, "resp_explicit_only", nil)
	if _, err := store.StartSession(context.Background(), "dev", previous); err != nil {
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
	outcome, err := reduce(context.Background(), state, checkpointLoaded{record: previous, resolution: session.HistoryUniqueHead, current: true}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, failed := outcome.nextState.phase.(failedPhase); failed {
		t.Fatalf("explicit-only resume failed: %#v", outcome.nextState.phase)
	}
	if outcome.nextState.advance != nil {
		t.Fatal("explicit-only lineage incorrectly synthesized a partial history digest")
	}
	if outcome.nextState.sessionID != previous.SessionID || outcome.nextState.expectedHead != previous.ID {
		t.Fatalf("explicit-only lineage lost CAS identity: session=%q head=%q", outcome.nextState.sessionID, outcome.nextState.expectedHead)
	}
}

func TestProviderCallCarriesCompleteRequestPlusExactResponsesData(t *testing.T) {
	target := provider.NewTargetSnapshot("target-a", "openai", "https://api.openai.test", "cred", "responses", "", "responses")
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
	resolved, err := session.Resume(current, session.Checkpoint{Request: previousRequest, Response: response})
	if err != nil {
		t.Fatal(err)
	}
	id, start, end, ok := resolved.ResponsesPrevious(target.TargetID, target.TargetVersion)
	if !ok {
		t.Fatal("matching target did not expose Responses data")
	}
	request := provider.Request{Canonical: resolved.Request(), ResponsesPrevious: &provider.ResponsesPrevious{ProviderResponseID: id, OmitStart: start, OmitEnd: end}}
	if len(request.Canonical.Items()) != 3 || request.ResponsesPrevious.OmitStart != 0 || request.ResponsesPrevious.OmitEnd != 2 {
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

func testSessionCheckpoint(t *testing.T, id canonical.SwobuResponseID, history *historyfingerprint.History) session.Checkpoint {
	t.Helper()
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "a", nil, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	scheme := testHistoryScheme
	if history != nil {
		scheme = history.Scheme()
	}
	return session.Checkpoint{ID: id, HistoryScheme: scheme, History: history, Request: testCanonicalRequest("a"), Response: response}
}
