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
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/replay"
)

type failingReplayStore struct{}

func (failingReplayStore) Get(context.Context, string, canonical.SwobuResponseID) (replay.Record, bool, error) {
	return replay.Record{}, false, nil
}

func (failingReplayStore) Put(context.Context, string, replay.Record) error {
	return errors.New("forced replay store failure")
}

// TestRunner_SwobuResponseIDReplacesProviderID proves that when the exchange
// pipeline wires a ResponseID through, the client-visible response body shows
// the Swobu ID instead of the provider-native one.
func TestRunner_SwobuResponseIDReplacesProviderID(t *testing.T) {
	// The provider ingress contains provider-native ID "resp_1".
	store := replay.NewMemoryStore()
	runner := withRuntime(bufferedProviderTransport(nil)).
		WithReplayStore(store)
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
		t.Fatal("expected replay record to be committed")
	}
	if rec.Response.Response().SwobuID.String() != "swobu_test_ex" {
		t.Fatalf("stored response id=%q, want swobu_test_ex", rec.Response.Response().SwobuID.String())
	}
}

func TestRunner_SwobuResponseIDPassedByInput(t *testing.T) {
	store := replay.NewMemoryStore()
	runner := withRuntime(bufferedProviderTransport(nil)).
		WithReplayStore(store)
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

func TestRunnerWithoutReplayStoreRejectsBeforeProviderIngress(t *testing.T) {
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
		SwobuResponseIDs: deterministicSwobuResponseIDGenerator{},
	}

	_, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID:       "no_replay_store",
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
			providerTransport: func(_ context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
				calls++
				return bufferedProviderTransport(nil)(context.Background(), target, doc)
			},
		},
		SwobuResponseIDs: deterministicSwobuResponseIDGenerator{},
	}
	_, err := runPreparedProviderForTest(context.Background(), runner, ExchangeInput{
		ExchangeID: "no_replay_store_previous",
		Request: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model:            "m",
			Items:            []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
			PreviousResponse: &canonical.ResponseRef{SwobuID: "resp_prev"},
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

func TestRunnerRejectsEmptyReplayWorkspaceSlugBeforeProviderIngress(t *testing.T) {
	calls := 0
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerTransport: func(_ context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
				calls++
				return bufferedProviderTransport(nil)(context.Background(), target, doc)
			},
		},
		ReplayStore:      replay.NewMemoryStore(),
		SwobuResponseIDs: deterministicSwobuResponseIDGenerator{},
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
		t.Fatal("expected empty replay workspace slug to reject before provider ingress")
	}
	if !strings.Contains(err.Error(), "replay workspace slug is required") {
		t.Fatalf("error = %v, want replay workspace slug rejection", err)
	}
	if calls != 0 {
		t.Fatalf("provider ingress calls = %d, want 0", calls)
	}
}

func TestRunnerWithReplayStoreAllocatesResponseIDWhenInputMissing(t *testing.T) {
	store := replay.NewMemoryStore()
	runner := withRuntime(bufferedProviderTransport(nil)).
		WithReplayStore(store).
		WithSwobuResponseIDs(deterministicSwobuResponseIDGenerator{})

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
		t.Fatal("expected replay record to be committed")
	}
	if rec.Response.Response().SwobuID.String() != "swobu_alloc_missing" {
		t.Fatalf("stored response id = %q, want swobu_alloc_missing", rec.Response.Response().SwobuID.String())
	}
}

func TestRunnerReplayCommitFailureDoesNotReturnSuccessfulBufferedBody(t *testing.T) {
	runner := withRuntime(bufferedProviderTransport(nil)).
		WithReplayStore(failingReplayStore{}).
		WithSwobuResponseIDs(deterministicSwobuResponseIDGenerator{})

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
	if readErr == nil || !IsReplayCommitFailure(readErr) {
		t.Fatalf("read error = %v, want replay commit failure", readErr)
	}
	if strings.Contains(string(raw), "ok") || strings.Contains(string(raw), "response.completed") {
		t.Fatalf("commit failure returned successful-looking body: %s", string(raw))
	}
}
