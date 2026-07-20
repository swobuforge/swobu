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

type failingCheckpointStore struct{}

func (failingCheckpointStore) Get(context.Context, string, canonical.SwobuResponseID) (session.Checkpoint, bool, error) {
	return session.Checkpoint{}, false, nil
}

func (failingCheckpointStore) Put(context.Context, string, session.Checkpoint) error {
	return errors.New("forced checkpoint store failure")
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

func TestRunnerCheckpointCommitFailureDoesNotReturnSuccessfulBufferedBody(t *testing.T) {
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
	if readErr == nil || !IsCheckpointCommitFailure(readErr) {
		t.Fatalf("read error = %v, want checkpoint commit failure", readErr)
	}
	if strings.Contains(string(raw), "ok") || strings.Contains(string(raw), "response.completed") {
		t.Fatalf("commit failure returned successful-looking body: %s", string(raw))
	}
}
