package exchange

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
)

func TestDefaultResponseIDGeneratorAllocatesPrefixedID(t *testing.T) {
	gen := NewDefaultResponseIDGenerator()
	id, err := gen.NewSwobuResponseID(context.Background(), "ex-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(string(id), "resp_") {
		t.Fatalf("expected resp_ prefix, got %q", id)
	}
	suffix := strings.TrimPrefix(string(id), "resp_")
	if len(suffix) != 32 {
		t.Fatalf("expected 32 hex chars after resp_, got %q", id)
	}
	if _, err := hex.DecodeString(suffix); err != nil {
		t.Fatalf("expected hex suffix, got %q: %v", id, err)
	}
}

type failingCheckpointStore struct{ puts *int }

func (failingCheckpointStore) Get(context.Context, string, canonical.SwobuResponseID) (session.Checkpoint, bool, error) {
	return session.Checkpoint{}, false, nil
}

func (s failingCheckpointStore) Put(context.Context, string, session.Checkpoint) error {
	if s.puts != nil {
		*s.puts++
	}
	return errors.New("forced checkpoint store failure at /private/checkpoints")
}

func (failingCheckpointStore) FindByHistory(context.Context, string, historyfingerprint.History) (session.HistoryMatch, error) {
	return session.MissingHistoryMatch(), nil
}

// TestRunner_SwobuResponseIDReplacesProviderID proves that when the exchange
// pipeline wires a ResponseID through, the client-visible response body shows
// the Swobu ID instead of the provider-native one.
func TestRunner_SwobuResponseIDReplacesProviderID(t *testing.T) {
	// The provider ingress contains provider-native ID "resp_1".
	store := session.NewMemoryStore()
	runner := withRuntime(bufferedProviderTransport(nil)).
		WithCheckpointStore(store)
	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "test_ex",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    "alpha",
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if ClientTransportForTest(out).Body == nil {
		t.Fatal("buffered response must set transport body")
	}
	raw, readErr := io.ReadAll(ClientTransportForTest(out).Body)
	if readErr != nil {
		t.Fatalf("read buffered body: %v", readErr)
	}

	// Client-visible response ID must be the Swobu ID, not the provider ID.
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	gotID, _ := body["id"].(string)
	if gotID != "swobu_test_ex" {
		t.Fatalf("client response id=%q, want swobu_test_ex (raw body: %s)", gotID, string(raw))
	}
	// Provider-native ID must NOT leak.
	if strings.Contains(string(raw), `"resp_1"`) {
		t.Fatalf("provider-native ID leaked to client: %s", string(raw))
	}

	rec, ok, err := store.Get(context.Background(), "alpha", canonical.NewSwobuResponseID("swobu_test_ex"))
	if err != nil {
		t.Fatalf("store.Get error: %v", err)
	}
	if !ok {
		t.Fatal("expected checkpoint to be committed")
	}
	if rec.Response.Response().SwobuID.String() != "swobu_test_ex" {
		t.Fatalf("stored response id=%q, want swobu_test_ex", rec.Response.Response().SwobuID.String())
	}
}

func TestRunner_SwobuResponseIDPassedByInput(t *testing.T) {
	store := session.NewMemoryStore()
	runner := withRuntime(bufferedProviderTransport(nil)).
		WithCheckpointStore(store)
	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "ex_gen",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    "alpha",
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if ClientTransportForTest(out).Body == nil {
		t.Fatal("body must not be nil")
	}
	raw, _ := io.ReadAll(ClientTransportForTest(out).Body)
	var body map[string]any
	json.Unmarshal(raw, &body)
	if body["id"] != "swobu_ex_gen" {
		t.Fatalf("id=%q, want swobu_ex_gen", body["id"])
	}
}

func TestRunnerWithoutCheckpointStoreRejectsBeforeProviderIngress(t *testing.T) {
	calls := 0
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerTransport: func(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
				calls++
				return provider.StreamIngress{Stream: carrier.ByteStream{MediaType: "text/event-stream",
					Body: io.NopCloser(strings.NewReader("event: response.completed\ndata: {}\n\n")),
				}}, nil
			},
		},
		ResponseIDs: deterministicResponseIDGenerator{},
	}

	_, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "no_checkpoint_store",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    "alpha",
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err == nil {
		t.Fatal("expected missing checkpoint store to reject before provider ingress")
	}
	if !strings.Contains(err.Error(), "checkpoint store is required") {
		t.Fatalf("error = %v, want checkpoint store rejection", err)
	}
	if calls != 0 {
		t.Fatalf("provider ingress calls = %d, want 0", calls)
	}
}

func TestRunnerWithoutCheckpointStoreRejectsPreviousResponseID(t *testing.T) {
	calls := 0
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerTransport: func(_ context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
				calls++
				return bufferedProviderTransport(nil)(context.Background(), target, doc)
			},
		},
		ResponseIDs: deterministicResponseIDGenerator{},
	}
	_, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID: "no_checkpoint_store_previous",
		Request: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model:            canonical.Specify("m"),
			Items:            []canonical.CanonicalItem{testMessage(canonical.MessageRoleUser, "hi")},
			PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_prev"},
		}),
	})
	if err == nil {
		t.Fatal("expected previous_response_id to reject when checkpoint store is missing")
	}
	if !strings.Contains(err.Error(), "checkpoint store is required") {
		t.Fatalf("error = %v, want checkpoint store rejection", err)
	}
	if calls != 0 {
		t.Fatalf("provider ingress calls = %d, want 0", calls)
	}
}

func TestRunnerRejectsEmptyCheckpointWorkspaceSlugBeforeProviderIngress(t *testing.T) {
	calls := 0
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerTransport: func(_ context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
				calls++
				return bufferedProviderTransport(nil)(context.Background(), target, doc)
			},
		},
		CheckpointStore: session.NewMemoryStore(),
		ResponseIDs:     deterministicResponseIDGenerator{},
	}
	_, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "empty_scope_ns",
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    "",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err == nil {
		t.Fatal("expected empty checkpoint workspace slug to reject before provider ingress")
	}
	if !strings.Contains(err.Error(), "checkpoint workspace slug is required") {
		t.Fatalf("error = %v, want checkpoint workspace slug rejection", err)
	}
	if calls != 0 {
		t.Fatalf("provider ingress calls = %d, want 0", calls)
	}
}

func TestRunnerWithCheckpointStoreAllocatesResponseIDWhenInputMissing(t *testing.T) {
	store := session.NewMemoryStore()
	runner := withRuntime(bufferedProviderTransport(nil)).
		WithCheckpointStore(store).
		WithResponseIDs(deterministicResponseIDGenerator{})

	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "alloc_missing",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    "alpha",
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if ClientTransportForTest(out).Body == nil {
		t.Fatal("buffered response must set transport body")
	}
	raw, readErr := io.ReadAll(ClientTransportForTest(out).Body)
	if readErr != nil {
		t.Fatalf("read buffered body: %v", readErr)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	gotID, _ := body["id"].(string)
	if gotID != "swobu_alloc_missing" {
		t.Fatalf("allocated response id = %q, want swobu_alloc_missing", gotID)
	}
	rec, ok, err := store.Get(context.Background(), "alpha", canonical.NewSwobuResponseID("swobu_alloc_missing"))
	if err != nil {
		t.Fatalf("store.Get error: %v", err)
	}
	if !ok {
		t.Fatal("expected checkpoint to be committed")
	}
	if rec.Response.Response().SwobuID.String() != "swobu_alloc_missing" {
		t.Fatalf("stored response id = %q, want swobu_alloc_missing", rec.Response.Response().SwobuID.String())
	}
}

func TestRunnerCheckpointCommitFailureRejectsBufferedBodyBeforePublication(t *testing.T) {
	runner := withRuntime(bufferedProviderTransport(nil)).
		WithCheckpointStore(failingCheckpointStore{}).
		WithResponseIDs(deterministicResponseIDGenerator{})

	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "commit_failure",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    "alpha",
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want delivery-owned terminal failure", err)
	}
	raw, readErr := io.ReadAll(ClientTransportForTest(out).Body)
	if readErr == nil || !strings.Contains(readErr.Error(), "checkpoint store failed") {
		t.Fatalf("read = (%q, %v), want checkpoint store failure before publication", raw, readErr)
	}
	var checkpointErr CheckpointCommitError
	if !errors.As(readErr, &checkpointErr) {
		t.Fatalf("read error type = %T, want CheckpointCommitError", readErr)
	}
	if len(raw) != 0 {
		t.Fatalf("published body on failed checkpoint commit: %s", raw)
	}
}

func TestRunnerCheckpointCommitFailureReplacesStreamingTerminalSuccess(t *testing.T) {
	providerCalls := 0
	checkpointPuts := 0
	transport := streamingProviderTransport(io.NopCloser(strings.NewReader("ignored")))
	runner := withRuntime(func(ctx context.Context, target provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
		providerCalls++
		return transport(ctx, target, document)
	}).
		WithCheckpointStore(failingCheckpointStore{puts: &checkpointPuts}).
		WithResponseIDs(deterministicResponseIDGenerator{})

	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "stream_commit_failure",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    "alpha",
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses_stream"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want delivery-owned terminal failure", err)
	}
	raw, readErr := io.ReadAll(ClientTransportForTest(out).Body)
	if readErr == nil || !strings.Contains(readErr.Error(), "checkpoint store failed") {
		t.Fatalf("stream read = (%q, %v), want terminal checkpoint failure", raw, readErr)
	}
	if len(raw) == 0 {
		t.Fatal("stream checkpoint failure erased already-published non-terminal chunks")
	}
	if strings.Contains(string(raw), `"status":"completed"`) {
		t.Fatalf("published terminal success on failed checkpoint commit: %s", raw)
	}
	if strings.Contains(string(raw), "/private/checkpoints") || strings.Contains(string(raw), "forced checkpoint") {
		t.Fatalf("public terminal exposed checkpoint implementation cause: %s", raw)
	}
	if providerCalls != 1 {
		t.Fatalf("provider calls = %d, want one without checkpoint retry", providerCalls)
	}
	if checkpointPuts != 1 {
		t.Fatalf("checkpoint puts = %d, want one", checkpointPuts)
	}
}

func TestCheckpointCommitFailureProjectsEveryProtocolFailureTerminal(t *testing.T) {
	tests := []struct {
		name      string
		family    canonical.ClientFamily
		forbidden []string
		required  []string
	}{
		{name: "chat completions", family: canonical.ClientFamilyChatCompletions, forbidden: []string{`"finish_reason"`}, required: []string{`"code":"INTERNAL_ERROR"`, `"type":"swobu_stream_error"`, "data: [DONE]"}},
		{name: "messages", family: canonical.ClientFamilyMessages, forbidden: []string{"event: message_delta", "event: message_stop", `"type":"message_stop"`}, required: []string{"event: error", `"code":"INTERNAL_ERROR"`}},
		{name: "responses", family: canonical.ClientFamilyResponses, forbidden: []string{"event: response.completed", `"type":"response.completed"`}, required: []string{`"type":"response.failed"`, `"code":"INTERNAL_ERROR"`}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := Runner{
				Runtime: runtimeWithProviderIngress{
					RuntimeResolver:   codecresolver.NewRuntimeCodecResolver(),
					providerTransport: streamingProviderTransport(io.NopCloser(strings.NewReader("ignored"))),
				},
				CheckpointStore: failingCheckpointStore{},
				ResponseIDs:     deterministicResponseIDGenerator{},
				Policy:          DefaultWorkspacePolicy(),
			}
			out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
				ExchangeID:       "marker_" + strings.ReplaceAll(test.name, " ", "_"),
				ClientFamily:     test.family,
				ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
				Request:          testCanonicalRequest("m"),
				WorkspaceSlug:    "alpha",
				ProviderProtocol: protocolkind.Responses,
				ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
				Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses_stream"),
				Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
			})
			if err != nil {
				t.Fatal(err)
			}
			raw, readErr := io.ReadAll(ClientTransportForTest(out).Body)
			if readErr == nil || !strings.Contains(readErr.Error(), "checkpoint store failed") {
				t.Fatalf("stream read = (%q, %v), want checkpoint failure", raw, readErr)
			}
			if len(raw) == 0 {
				t.Fatal("checkpoint failure erased every non-terminal protocol frame")
			}
			for _, marker := range test.forbidden {
				if strings.Contains(string(raw), marker) {
					t.Fatalf("published terminal marker %q before checkpoint commit:\n%s", marker, raw)
				}
			}
			for _, marker := range test.required {
				if !strings.Contains(string(raw), marker) {
					t.Fatalf("missing failure terminal marker %q:\n%s", marker, raw)
				}
			}
		})
	}
}

func TestRunnerCheckpointCommitFailureReplacesMessageTerminalSuccess(t *testing.T) {
	runner := withRuntime(bufferedProviderTransport(nil)).
		WithCheckpointStore(failingCheckpointStore{}).
		WithResponseIDs(deterministicResponseIDGenerator{})

	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "message_commit_failure",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingWebSocket),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    "alpha",
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContractForDeliveries(delivery.StreamingDelivery(delivery.FramingWebSocket), delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want delivery-owned terminal failure", err)
	}
	stream := ClientMessageTransportForTest(out).Messages
	published := 0
	for {
		message, nextErr := stream.Next(context.Background())
		if nextErr != nil {
			if !strings.Contains(nextErr.Error(), "checkpoint store failed") {
				t.Fatalf("message terminal error = %v", nextErr)
			}
			break
		}
		published++
		if strings.Contains(string(message), `"status":"completed"`) {
			t.Fatalf("published terminal success on failed checkpoint commit: %s", message)
		}
	}
	if published == 0 {
		t.Fatal("message checkpoint failure erased already-published non-terminal messages")
	}
}

func TestCheckpointCommitFailureSuppressesResponsesWebSocketTerminalMessage(t *testing.T) {
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver:   codecresolver.NewRuntimeCodecResolver(),
			providerTransport: bufferedProviderTransport(nil),
		},
		CheckpointStore: failingCheckpointStore{},
		ResponseIDs:     deterministicResponseIDGenerator{},
		Policy:          DefaultWorkspacePolicy(),
	}
	out, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "responses_websocket_marker",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingWebSocket),
		Request:          testCanonicalRequest("m"),
		WorkspaceSlug:    "alpha",
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           provider.NewTargetSnapshot("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContractForDeliveries(delivery.StreamingDelivery(delivery.FramingWebSocket), delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatal(err)
	}
	stream := ClientMessageTransportForTest(out).Messages
	published := 0
	for {
		message, nextErr := stream.Next(context.Background())
		if nextErr != nil {
			if !strings.Contains(nextErr.Error(), "checkpoint store failed") {
				t.Fatalf("websocket terminal error = %v", nextErr)
			}
			break
		}
		published++
		if strings.Contains(string(message), `"type":"response.completed"`) {
			t.Fatalf("published response.completed before checkpoint commit: %s", message)
		}
	}
	if published == 0 {
		t.Fatal("checkpoint failure erased every non-terminal websocket message")
	}
}
