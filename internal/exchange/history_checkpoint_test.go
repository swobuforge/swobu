package exchange

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestImplicitFingerprintLookupPreservesFullHistoryAndAddsExactTargetDelta(t *testing.T) {
	store := session.NewMemoryStore()
	target := provider.NewTargetSnapshot("openai", "target-a", "https://example.test", "cred", protocolkind.Responses, "m", "responses")
	target.TargetVersion = 7
	image, err := canonical.NewURLImage("https://example.test/mutable.png", canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	imageMessage, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewTextMessagePart("one"), canonical.NewImageMessagePart(image),
	})
	if err != nil {
		t.Fatal(err)
	}
	previousRequest := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{imageMessage}})
	previousMedia, err := (session.ResolvedMedia{}).Bind(
		canonical.RequestPartRef{Item: 0, Part: 1}, "https://example.test/mutable.png",
		canonical.ImageMediaPNG, []byte("durable image bytes"),
	)
	if err != nil {
		t.Fatal(err)
	}
	previousResponse, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: "resp_previous", Responses: &canonical.ResponsesNativeRef{
		ProviderResponseID: "provider_previous", TargetID: target.TargetID, TargetVersion: target.TargetVersion,
	}}, "m", []canonical.CanonicalItem{testMessage(canonical.MessageRoleAssistant, "answer")}, "completed", canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	previousFingerprint := testExchangeHistoryFingerprint(t, "responses", "previous")
	if err := store.Put(context.Background(), "dev", session.Checkpoint{HistoryFingerprint: &previousFingerprint, Request: previousRequest, Response: previousResponse, ResolvedMedia: previousMedia}); err != nil {
		t.Fatal(err)
	}
	full := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{
		imageMessage,
		testMessage(canonical.MessageRoleAssistant, "answer"),
		testMessage(canonical.MessageRoleUser, "two"),
	}})
	s := reducerTestState(t)
	s.input.request = full
	s.input.rebasedRequest = &wire.RebasedRequest{Previous: previousFingerprint, Request: canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "two")},
	})}
	s.input.requestFingerprint = testHistoryRequest([]byte("two"))
	runner := reducerRuntime()
	runner.CheckpointStore = store

	started, err := reduceStarting(s, exchangeStarted{}, runner)
	if err != nil {
		t.Fatal(err)
	}
	command, ok := started.command.(loadCheckpointCommand)
	if !ok || command.explicit || command.history != previousFingerprint {
		t.Fatalf("implicit command = %#v", started.command)
	}
	loaded := executeCommand(context.Background(), command)
	resolved, err := reduceLoadingCheckpoint(started.nextState, started.nextState.phase.(loadingCheckpointPhase), loaded, runner)
	if err != nil {
		t.Fatal(err)
	}
	prepared := resolved.nextState.prepared
	if prepared == nil || len(prepared.Full.Items()) != 3 {
		t.Fatalf("full request = %#v, want supplied three-item history", prepared)
	}
	native := prepared.ForTarget(target)
	if len(native.Items()) != 1 {
		t.Fatalf("native delta item count = %d, want current contribution only", len(native.Items()))
	}
	if previous, ok := native.PreviousResponse(); !ok || previous.SwobuID != "resp_previous" {
		t.Fatalf("native predecessor = %#v", previous)
	}
	unusableTarget := target
	unusableTarget.TargetVersion++
	if fallback := prepared.ForTarget(unusableTarget); len(fallback.Items()) != 3 {
		t.Fatalf("unusable hit fallback items = %d, want complete supplied history", len(fallback.Items()))
	}
	if resolved.nextState.advance == nil || resolved.nextState.advance.Previous == nil || *resolved.nextState.advance.Previous != previousFingerprint {
		t.Fatal("implicit predecessor was not retained as child composition base")
	}
	if asset, ok := prepared.ResolvedMedia.Resolve(canonical.RequestPartRef{Item: 0, Part: 1}, "https://example.test/mutable.png"); !ok || string(asset.Bytes()) != "durable image bytes" {
		t.Fatalf("implicit resolved media = (%q, %t)", asset.Bytes(), ok)
	}

	nextResponse, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "resp_next"}, "m",
		[]canonical.CanonicalItem{testMessage(canonical.MessageRoleAssistant, "next answer")},
		"completed", canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	committer := &checkpointCommitter{
		exchangeID: "next", workspaceSlug: "dev", store: store, maxBytes: 1 << 20,
		request: prepared.Full, resolvedMedia: prepared.ResolvedMedia,
		capture: &checkpointCaptureResponseStream{result: checkpointCaptureSnapshot{
			state: checkpointCaptureCompleted, response: nextResponse,
		}},
	}
	if err := committer.commitDocument(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	nextCheckpoint, found, err := store.Get(context.Background(), "dev", "resp_next")
	if err != nil || !found {
		t.Fatalf("next checkpoint = (%t, %v)", found, err)
	}
	if asset, ok := nextCheckpoint.ResolvedMedia.Resolve(canonical.RequestPartRef{Item: 0, Part: 1}, "https://example.test/mutable.png"); !ok || string(asset.Bytes()) != "durable image bytes" {
		t.Fatalf("next checkpoint media = (%q, %t)", asset.Bytes(), ok)
	}
}

func TestImplicitFingerprintMissExecutesFullRequestAndRetainsCompositionBase(t *testing.T) {
	missing := testExchangeHistoryFingerprint(t, "responses", "missing")
	full := testCanonicalRequest("m")
	s := reducerTestState(t)
	s.input.request = full
	s.input.rebasedRequest = &wire.RebasedRequest{Previous: missing, Request: full.Clone()}
	s.input.requestFingerprint = testHistoryRequest([]byte("current"))
	runner := reducerRuntime()
	started, err := reduceStarting(s, exchangeStarted{}, runner)
	if err != nil {
		t.Fatal(err)
	}
	command := started.command.(loadCheckpointCommand)
	loaded := executeCommand(context.Background(), command)
	resolved, err := reduceLoadingCheckpoint(started.nextState, started.nextState.phase.(loadingCheckpointPhase), loaded, runner)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.nextState.prepared.ForTarget(provider.TargetSnapshot{}); len(got.Items()) != len(full.Items()) {
		t.Fatalf("miss request items = %d, want %d", len(got.Items()), len(full.Items()))
	}
	if resolved.nextState.advance == nil || resolved.nextState.advance.Previous == nil || *resolved.nextState.advance.Previous != missing {
		t.Fatal("miss did not retain decoded predecessor as composition base")
	}
}

func TestExplicitPredecessorTakesPriorityOverImplicitFingerprint(t *testing.T) {
	implicit := testExchangeHistoryFingerprint(t, "responses", "implicit")
	s := reducerTestState(t)
	s.input.request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "two")},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_explicit"},
	})
	s.input.rebasedRequest = &wire.RebasedRequest{Previous: implicit, Request: testCanonicalRequest("m")}
	started, err := reduceStarting(s, exchangeStarted{}, reducerRuntime())
	if err != nil {
		t.Fatal(err)
	}
	command := started.command.(loadCheckpointCommand)
	if !command.explicit || command.reference != "resp_explicit" || command.history != (historyfingerprint.History{}) {
		t.Fatalf("explicit precedence command = %#v", command)
	}
}

func TestExplicitSameSchemePredecessorCreatesCompleteHistoryAdvance(t *testing.T) {
	store := session.NewMemoryStore()
	parent := testExchangeHistoryFingerprint(t, "responses", "parent")
	parentResponse, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "resp_explicit"}, "m", nil,
		"completed", canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "dev", session.Checkpoint{
		HistoryFingerprint: &parent, Response: parentResponse,
	}); err != nil {
		t.Fatal(err)
	}
	currentRequest := testHistoryRequest([]byte("all-current-input"))
	s := reducerTestState(t)
	s.input.request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "current")},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_explicit"},
	})
	s.input.requestFingerprint = currentRequest
	runner := reducerRuntime()
	runner.CheckpointStore = store
	started, err := reduceStarting(s, exchangeStarted{}, runner)
	if err != nil {
		t.Fatal(err)
	}
	loaded := executeCommand(context.Background(), started.command)
	resolved, err := reduceLoadingCheckpoint(started.nextState, started.nextState.phase.(loadingCheckpointPhase), loaded, runner)
	if err != nil {
		t.Fatal(err)
	}
	advance := resolved.nextState.advance
	if advance == nil || advance.Previous == nil || *advance.Previous != parent || advance.Request != currentRequest {
		t.Fatalf("history advance = %#v, want parent plus complete request leaf", advance)
	}
	responseLeaf := testHistoryResponse([]byte("response"))
	gotChild, err := historyfingerprint.Advance(advance.Previous, advance.Request, *responseLeaf)
	if err != nil {
		t.Fatal(err)
	}
	wantChild, err := historyfingerprint.Advance(&parent, currentRequest, *responseLeaf)
	if err != nil {
		t.Fatal(err)
	}
	if gotChild != wantChild {
		t.Fatalf("explicit child = %#v, want %#v", gotChild, wantChild)
	}
}

func TestExplicitCrossSchemePredecessorStoresNoFalseFingerprintRoot(t *testing.T) {
	store := session.NewMemoryStore()
	checkpointFingerprint := testExchangeHistoryFingerprint(t, "messages", "messages-chain")
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: "resp_explicit"}, "m", nil, "completed", canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "dev", session.Checkpoint{HistoryFingerprint: &checkpointFingerprint, Response: response}); err != nil {
		t.Fatal(err)
	}
	s := reducerTestState(t)
	s.input.request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "two")},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_explicit"},
	})
	s.input.requestFingerprint = testHistoryRequest([]byte("two"))
	runner := reducerRuntime()
	runner.CheckpointStore = store
	started, err := reduceStarting(s, exchangeStarted{}, runner)
	if err != nil {
		t.Fatal(err)
	}
	command := started.command.(loadCheckpointCommand)
	loaded := executeCommand(context.Background(), command)
	resolved, err := reduceLoadingCheckpoint(started.nextState, started.nextState.phase.(loadingCheckpointPhase), loaded, runner)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.nextState.advance != nil {
		t.Fatalf("cross-scheme explicit predecessor became composable: %#v", resolved.nextState.advance)
	}
}

func TestBufferedCheckpointCommitsOnlyAfterClientEncoding(t *testing.T) {
	store := session.NewMemoryStore()
	runner := withRuntime(bufferedProviderTransport(nil)).WithCheckpointStore(store)
	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID: "fingerprint_commit", ClientFamily: canonical.ClientFamilyResponses,
		ClientDelivery: delivery.BufferedDelivery(), Request: testCanonicalRequest("m"), WorkspaceSlug: "alpha",
		ProviderProtocol: protocolkind.Responses, ProviderDelivery: delivery.BufferedDelivery(),
		Target:   provider.NewTargetSnapshot("openai", "target-a", "https://example.test", "cred", protocolkind.Responses, "m", "responses"),
		Contract: NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(context.Background(), "alpha", "swobu_fingerprint_commit"); err != nil || found {
		t.Fatalf("checkpoint before body encoding = (%t, %v), want absent", found, err)
	}
	if _, err := io.ReadAll(ClientTransportForTest(out).Body); err != nil {
		t.Fatal(err)
	}
	record, found, err := store.Get(context.Background(), "alpha", "swobu_fingerprint_commit")
	if err != nil || !found || record.HistoryFingerprint == nil {
		t.Fatalf("checkpoint after body encoding = (%#v, %t, %v)", record.HistoryFingerprint, found, err)
	}
	indexed, found, err := store.FindByHistory(context.Background(), "alpha", *record.HistoryFingerprint)
	if err != nil || !found || indexed.Response.Response().SwobuID != record.Response.Response().SwobuID {
		t.Fatalf("fingerprint index lookup = (%q, %t, %v)", indexed.Response.Response().SwobuID, found, err)
	}
}

func TestStreamingCheckpointCommitsOnlyAfterTerminalClientEncoding(t *testing.T) {
	store := session.NewMemoryStore()
	runner := withRuntime(streamingProviderTransport(io.NopCloser(strings.NewReader("ignored")))).WithCheckpointStore(store)
	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID: "fingerprint_stream", ClientFamily: canonical.ClientFamilyResponses,
		ClientDelivery: delivery.StreamingDelivery(delivery.FramingSSE), Request: testCanonicalRequest("m"), WorkspaceSlug: "alpha",
		ProviderProtocol: protocolkind.Responses, ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:   provider.NewTargetSnapshot("openai", "target-a", "https://example.test", "cred", protocolkind.Responses, "m", "responses_stream"),
		Contract: NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(context.Background(), "alpha", "swobu_fingerprint_stream"); err != nil || found {
		t.Fatalf("checkpoint before terminal stream encoding = (%t, %v), want absent", found, err)
	}
	if _, err := io.ReadAll(ClientTransportForTest(out).Body); err != nil {
		t.Fatal(err)
	}
	record, found, err := store.Get(context.Background(), "alpha", "swobu_fingerprint_stream")
	if err != nil || !found || record.HistoryFingerprint == nil {
		t.Fatalf("checkpoint after terminal stream encoding = (%#v, %t, %v)", record.HistoryFingerprint, found, err)
	}
}

func TestMessageCheckpointCommitsOnlyAfterTerminalClientEncoding(t *testing.T) {
	store := session.NewMemoryStore()
	runner := withRuntime(bufferedProviderTransport(nil)).WithCheckpointStore(store)
	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID: "fingerprint_messages", ClientFamily: canonical.ClientFamilyResponses,
		ClientDelivery: delivery.StreamingDelivery(delivery.FramingWebSocket), Request: testCanonicalRequest("m"), WorkspaceSlug: "alpha",
		ProviderProtocol: protocolkind.Responses, ProviderDelivery: delivery.BufferedDelivery(),
		Target:   provider.NewTargetSnapshot("openai", "target-a", "https://example.test", "cred", protocolkind.Responses, "m", "responses"),
		Contract: NewExecutionContractForDeliveries(delivery.StreamingDelivery(delivery.FramingWebSocket), delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(context.Background(), "alpha", "swobu_fingerprint_messages"); err != nil || found {
		t.Fatalf("checkpoint before terminal message encoding = (%t, %v), want absent", found, err)
	}
	messages := ClientMessageTransportForTest(out).Messages
	for {
		_, nextErr := messages.Next(context.Background())
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
	}
	record, found, err := store.Get(context.Background(), "alpha", "swobu_fingerprint_messages")
	if err != nil || !found || record.HistoryFingerprint == nil {
		t.Fatalf("checkpoint after terminal message encoding = (%#v, %t, %v)", record.HistoryFingerprint, found, err)
	}
}

func TestClosingStreamBeforeTerminalEncodingDoesNotCommitCheckpoint(t *testing.T) {
	store := session.NewMemoryStore()
	runner := withRuntime(streamingProviderTransport(io.NopCloser(strings.NewReader("ignored")))).WithCheckpointStore(store)
	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID: "fingerprint_closed", ClientFamily: canonical.ClientFamilyResponses,
		ClientDelivery: delivery.StreamingDelivery(delivery.FramingSSE), Request: testCanonicalRequest("m"), WorkspaceSlug: "alpha",
		ProviderProtocol: protocolkind.Responses, ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:   provider.NewTargetSnapshot("openai", "target-a", "https://example.test", "cred", protocolkind.Responses, "m", "responses_stream"),
		Contract: NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ClientTransportForTest(out).Body.Close(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(context.Background(), "alpha", "swobu_fingerprint_closed"); err != nil || found {
		t.Fatalf("checkpoint after early close = (%t, %v), want absent", found, err)
	}
}

func TestCompletedResponseWithoutHistoryFingerprintStillCommitsExplicitCheckpoint(t *testing.T) {
	store := session.NewMemoryStore()
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "swobu_no_history"},
		"m",
		[]canonical.CanonicalItem{testMessage(canonical.MessageRoleAssistant, "ok")},
		"completed",
		canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	capture := &checkpointCaptureResponseStream{result: checkpointCaptureSnapshot{
		state: checkpointCaptureCompleted, response: response,
	}}
	committer := &checkpointCommitter{
		exchangeID: "no_history", workspaceSlug: "alpha", store: store, maxBytes: 1 << 20,
		request: testCanonicalRequest("m"), advance: &historyAdvance{Request: testHistoryRequest([]byte("request"))},
		capture: capture,
	}
	if err := committer.commitDocument(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	record, found, err := store.Get(context.Background(), "alpha", "swobu_no_history")
	if err != nil || !found {
		t.Fatalf("explicit checkpoint lookup = (%t, %v), want committed", found, err)
	}
	if record.HistoryFingerprint != nil {
		t.Fatalf("history fingerprint = %#v, want nil", record.HistoryFingerprint)
	}
}

func TestCheckpointCommitterRejectsOversizeCanonicalCheckpoint(t *testing.T) {
	store := session.NewMemoryStore()
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "swobu_oversize"},
		"m",
		[]canonical.CanonicalItem{testMessage(canonical.MessageRoleAssistant, "ok")},
		"completed",
		canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	committer := &checkpointCommitter{
		exchangeID: "oversize", workspaceSlug: "alpha", store: store, maxBytes: 1,
		request: testCanonicalRequest("m"),
		capture: &checkpointCaptureResponseStream{result: checkpointCaptureSnapshot{
			state: checkpointCaptureCompleted, response: response,
		}},
	}
	if err := committer.commitDocument(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "checkpoint size validation failed") {
		t.Fatalf("oversize commit error = %v", err)
	}
	if _, found, err := store.Get(context.Background(), "alpha", "swobu_oversize"); err != nil || found {
		t.Fatalf("oversize checkpoint lookup = (%t, %v), want absent", found, err)
	}
}

func TestHistoryComposeFailureUsesOptionalIndexDiagnosticAndStillCommits(t *testing.T) {
	store := session.NewMemoryStore()
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "swobu_compose_failure"}, "m",
		[]canonical.CanonicalItem{testMessage(canonical.MessageRoleAssistant, "ok")},
		"completed", canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	base := testExchangeHistoryFingerprint(t, "messages", "base")
	committer := &checkpointCommitter{
		exchangeID: "compose_failure", workspaceSlug: "alpha", store: store, maxBytes: 1 << 20,
		request: testCanonicalRequest("m"),
		advance: &historyAdvance{Previous: &base, Request: testHistoryRequest([]byte("request"))},
		capture: &checkpointCaptureResponseStream{result: checkpointCaptureSnapshot{
			state: checkpointCaptureCompleted, response: response,
		}},
	}
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previousLogger)
	if err := committer.commitDocument(context.Background(), testHistoryResponse([]byte("response"))); err != nil {
		t.Fatal(err)
	}
	record, found, err := store.Get(context.Background(), "alpha", "swobu_compose_failure")
	if err != nil || !found {
		t.Fatalf("explicit checkpoint = (%t, %v), want stored", found, err)
	}
	if record.HistoryFingerprint != nil {
		t.Fatalf("history fingerprint = %#v, want omitted", record.HistoryFingerprint)
	}
	if got := logs.String(); !strings.Contains(got, "event=history_fingerprint_compose_failed") || strings.Contains(got, "event=checkpoint_commit_failed") {
		t.Fatalf("compose diagnostic = %q", got)
	}
}

func testExchangeHistoryFingerprint(t *testing.T, scheme historyfingerprint.Scheme, material string) historyfingerprint.History {
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
