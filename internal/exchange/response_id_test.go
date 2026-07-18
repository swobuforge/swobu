package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/replay"
)

type failingReplayStore struct{}

func (failingReplayStore) Get(context.Context, replay.Scope, replay.ID) (replay.Record, bool, error) {
	return replay.Record{}, false, nil
}

func (failingReplayStore) Put(context.Context, replay.Scope, replay.Record) error {
	return errors.New("forced replay store failure")
}

// TestRunner_SwobuResponseIDReplacesProviderID proves that when the exchange
// pipeline wires a ResponseID through, the client-visible response body shows
// the Swobu ID instead of the provider-native one.
func TestRunner_SwobuResponseIDReplacesProviderID(t *testing.T) {
	// The provider ingress contains provider-native ID "resp_1".
	store := replay.NewMemoryStore()
	runner := withRuntime(bufferedProviderIngressResolver(nil)).
		WithReplayStore(store)
	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "test_ex",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      unsafeLocalReplayScope("alpha"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Transport.Body == nil {
		t.Fatal("buffered response must set transport body")
	}
	raw, readErr := io.ReadAll(out.Transport.Body)
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

	rec, ok, err := store.Get(context.Background(), unsafeLocalReplayScope("alpha"), replay.ReplayIDFromResponseID("swobu_test_ex"))
	if err != nil {
		t.Fatalf("store.Get error: %v", err)
	}
	if !ok {
		t.Fatal("expected replay record to be committed")
	}
	if rec.Response.ResultID() != "swobu_test_ex" {
		t.Fatalf("stored response id=%q, want swobu_test_ex", rec.Response.ResultID())
	}
}

func TestRunner_SwobuResponseIDPassedByInput(t *testing.T) {
	store := replay.NewMemoryStore()
	runner := withRuntime(bufferedProviderIngressResolver(nil)).
		WithReplayStore(store)
	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_gen",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      unsafeLocalReplayScope("alpha"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Transport.Body == nil {
		t.Fatal("body must not be nil")
	}
	raw, _ := io.ReadAll(out.Transport.Body)
	var body map[string]any
	json.Unmarshal(raw, &body)
	if body["id"] != "swobu_ex_gen" {
		t.Fatalf("id=%q, want swobu_ex_gen", body["id"])
	}
}

func TestRunnerWithoutReplayStoreRejectsBeforeProviderIngress(t *testing.T) {
	calls := 0
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerIngress: func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
				calls++
				return carrier.CarrierStream{
					Stage:   carrier.StageProviderIngressIn,
					Family:  req.Target.ProtocolKind,
					Framing: carrier.FramingSSE,
					Frames:  carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader("event: response.completed\ndata: {}\n\n"))),
				}, nil
			},
		},
		ResponseIDs: deterministicResponseIDGenerator{},
	}

	_, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "no_replay_store",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      unsafeLocalReplayScope("alpha"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err == nil {
		t.Fatal("expected missing replay store to reject before provider ingress")
	}
	if !strings.Contains(err.Error(), "replay store is required") {
		t.Fatalf("error = %v, want replay store rejection", err)
	}
	if calls != 0 {
		t.Fatalf("provider ingress calls = %d, want 0", calls)
	}
}

func TestRunnerWithoutReplayStoreRejectsPreviousResponseID(t *testing.T) {
	calls := 0
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerIngress: func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
				calls++
				return bufferedProviderIngressResolver(nil)(context.Background(), req)
			},
		},
		ResponseIDs: deterministicResponseIDGenerator{},
	}
	_, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID: "no_replay_store_previous",
		Request: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
			Turn:  canonical.NewTurnRef("resp_prev"),
		}),
	})
	if err == nil {
		t.Fatal("expected previous_response_id to reject when replay store is missing")
	}
	if !strings.Contains(err.Error(), "replay store is required") {
		t.Fatalf("error = %v, want replay store rejection", err)
	}
	if calls != 0 {
		t.Fatalf("provider ingress calls = %d, want 0", calls)
	}
}

func TestRunnerRejectsEmptyReplayScopeNamespaceBeforeProviderIngress(t *testing.T) {
	calls := 0
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerIngress: func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
				calls++
				return bufferedProviderIngressResolver(nil)(context.Background(), req)
			},
		},
		ReplayStore: replay.NewMemoryStore(),
		ResponseIDs: deterministicResponseIDGenerator{},
	}
	_, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "empty_scope_ns",
		Request:          testCanonicalRequest("m"),
		ReplayScope:      replay.Scope{CallerKey: "local"},
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err == nil {
		t.Fatal("expected empty replay scope namespace to reject before provider ingress")
	}
	if !strings.Contains(err.Error(), "replay scope namespace is required") {
		t.Fatalf("error = %v, want replay scope namespace rejection", err)
	}
	if calls != 0 {
		t.Fatalf("provider ingress calls = %d, want 0", calls)
	}
}

func TestRunnerRejectsEmptyReplayScopeCallerKeyBeforeProviderIngress(t *testing.T) {
	calls := 0
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerIngress: func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
				calls++
				return bufferedProviderIngressResolver(nil)(context.Background(), req)
			},
		},
		ReplayStore: replay.NewMemoryStore(),
		ResponseIDs: deterministicResponseIDGenerator{},
	}
	_, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "empty_scope_ck",
		Request:          testCanonicalRequest("m"),
		ReplayScope:      replay.Scope{Namespace: "alpha"},
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err == nil {
		t.Fatal("expected empty replay scope caller key to reject before provider ingress")
	}
	if !strings.Contains(err.Error(), "replay scope caller key is required") {
		t.Fatalf("error = %v, want replay scope caller key rejection", err)
	}
	if calls != 0 {
		t.Fatalf("provider ingress calls = %d, want 0", calls)
	}
}

func TestRunnerWithReplayStoreAllocatesResponseIDWhenInputMissing(t *testing.T) {
	store := replay.NewMemoryStore()
	runner := withRuntime(bufferedProviderIngressResolver(nil)).
		WithReplayStore(store).
		WithResponseIDs(deterministicResponseIDGenerator{})

	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "alloc_missing",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      unsafeLocalReplayScope("alpha"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if out.Transport.Body == nil {
		t.Fatal("buffered response must set transport body")
	}
	raw, readErr := io.ReadAll(out.Transport.Body)
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
	rec, ok, err := store.Get(context.Background(), unsafeLocalReplayScope("alpha"), replay.ReplayIDFromResponseID("swobu_alloc_missing"))
	if err != nil {
		t.Fatalf("store.Get error: %v", err)
	}
	if !ok {
		t.Fatal("expected replay record to be committed")
	}
	if rec.Response.ResultID() != "swobu_alloc_missing" {
		t.Fatalf("stored response id = %q, want swobu_alloc_missing", rec.Response.ResultID())
	}
}

func TestRunnerReplayCommitFailureDoesNotReturnSuccessfulBufferedBody(t *testing.T) {
	runner := withRuntime(bufferedProviderIngressResolver(nil)).
		WithReplayStore(failingReplayStore{}).
		WithResponseIDs(deterministicResponseIDGenerator{})

	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "commit_failure",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      unsafeLocalReplayScope("alpha"),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
		Target:           NewRoutableTarget("openai", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.BufferedDelivery()),
	})
	if err == nil {
		t.Fatalf("Run() error = nil, want replay commit failure")
	}
	if out.Transport.Body != nil {
		raw, readErr := io.ReadAll(out.Transport.Body)
		if readErr != nil {
			t.Fatalf("read failed body: %v", readErr)
		}
		if strings.Contains(string(raw), "ok") || strings.Contains(string(raw), "response.completed") {
			t.Fatalf("commit failure returned successful-looking body: %s", string(raw))
		}
	}
}
