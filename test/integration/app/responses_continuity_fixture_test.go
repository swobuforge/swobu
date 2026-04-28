package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/adapters/outbound/continuitystore"
	customadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/custom"
	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

func TestRequestPathHandler_UsesStoredThreadForBufferedResponsesContinuity(t *testing.T) {
	t.Parallel()

	fixture := mustReadResponsesContinuityFixtureApp(t, "openrouter_buffered_ok.json")
	var requestBodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		requestBodies = append(requestBodies, string(raw))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	store := continuitystore.NewLocalResponseContinuityStore(continuitystore.LocalResponseContinuityStoreConfig{
		Now: func() time.Time { return time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC) },
	})
	orchestrator, endpoint := newFixtureResponsesOrchestrator(t, upstream, store)

	first, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-buffered-first",
		Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:     "m",
			InputText: "hi",
		}),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("first HandleRequest returned error: %v", err)
	}
	firstOutput, ok := first.Response.Output().(compatibility.CanonicalOutputValue)
	if !ok {
		t.Fatalf("first output type = %T, want compatibility.CanonicalOutputValue", first.Response.Output())
	}
	bufferedFixtureResponseID := firstOutput.ResultID()
	if strings.TrimSpace(bufferedFixtureResponseID) == "" {
		t.Fatal("first buffered response missing result id")
	}

	snapshot, ok, err := store.Load(context.Background(), bufferedFixtureResponseID)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected stored continuity snapshot after first buffered response")
	}
	if len(snapshot.Thread) != 2 {
		t.Fatalf("stored thread len = %d, want 2", len(snapshot.Thread))
	}
	if got := snapshot.Thread[1].Text; !strings.EqualFold(got, "ok") {
		t.Fatalf("stored assistant text = %q, want case-insensitive %q", got, "ok")
	}

	_, err = orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-buffered-second",
		Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:              "m",
			PreviousResponseID: bufferedFixtureResponseID,
			InputText:          "continue",
		}),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("second HandleRequest returned error: %v", err)
	}

	if len(requestBodies) != 2 {
		t.Fatalf("upstream requests = %d, want 2", len(requestBodies))
	}
	if got := requestBodies[0]; got != `{"model":"m","input":"hi"}` {
		t.Fatalf("first upstream body = %q, want %q", got, `{"model":"m","input":"hi"}`)
	}
	wantSecondBody := fmt.Sprintf(`{"model":"m","input":"continue","previous_response_id":"%s"}`, bufferedFixtureResponseID)
	if got := requestBodies[1]; got != wantSecondBody {
		t.Fatalf("second upstream body = %q, want delta input with previous_response_id", got)
	}
}

func TestRequestPathHandler_UsesStoredThreadForStreamingResponsesContinuity(t *testing.T) {
	t.Parallel()

	fixture := mustReadResponsesContinuityFixtureApp(t, "openrouter_stream_ok.sse")
	var requestBodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		requestBodies = append(requestBodies, string(raw))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	store := continuitystore.NewLocalResponseContinuityStore(continuitystore.LocalResponseContinuityStoreConfig{
		Now: func() time.Time { return time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC) },
	})
	orchestrator, endpoint := newFixtureResponsesOrchestrator(t, upstream, store)

	first, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-stream-first",
		Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:     "m",
			InputText: "hi",
		}),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeStreaming),
	})
	if err != nil {
		t.Fatalf("first HandleRequest returned error: %v", err)
	}
	streamingFixtureResponseID := mustExtractResponsesSSEStartedIDApp(t, fixture)
	if err := drainOutputStream(first.Response); err != nil {
		t.Fatalf("drain first stream: %v", err)
	}

	snapshot, ok, err := store.Load(context.Background(), streamingFixtureResponseID)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected stored continuity snapshot after first streaming response")
	}
	if len(snapshot.Thread) != 2 {
		t.Fatalf("stored thread len = %d, want 2", len(snapshot.Thread))
	}
	if got := snapshot.Thread[1].Text; !strings.EqualFold(got, "ok") {
		t.Fatalf("stored assistant text = %q, want case-insensitive %q", got, "ok")
	}

	second, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-stream-second",
		Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:              "m",
			PreviousResponseID: streamingFixtureResponseID,
			InputText:          "continue",
		}),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeStreaming),
	})
	if err != nil {
		t.Fatalf("second HandleRequest returned error: %v", err)
	}
	if err := drainOutputStream(second.Response); err != nil {
		t.Fatalf("drain second stream: %v", err)
	}

	if len(requestBodies) != 2 {
		t.Fatalf("upstream requests = %d, want 2", len(requestBodies))
	}
	if got := requestBodies[0]; got != `{"model":"m","input":"hi","stream":true}` {
		t.Fatalf("first upstream body = %q, want %q", got, `{"model":"m","input":"hi","stream":true}`)
	}
	wantSecondBody := fmt.Sprintf(`{"model":"m","input":"continue","previous_response_id":"%s","stream":true}`, streamingFixtureResponseID)
	if got := requestBodies[1]; got != wantSecondBody {
		t.Fatalf("second upstream body = %q, want delta input with previous_response_id", got)
	}
}

func TestRequestPathHandler_RejectsMissingPreviousResponseIDBeforeProviderCall(t *testing.T) {
	t.Parallel()

	tests := []compatibility.DeliveryMode{
		compatibility.DeliveryModeBuffered,
		compatibility.DeliveryModeStreaming,
	}

	for _, deliveryMode := range tests {
		t.Run(string(deliveryMode), func(t *testing.T) {
			t.Parallel()

			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				t.Fatalf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
			}))
			defer upstream.Close()

			store := continuitystore.NewLocalResponseContinuityStore(continuitystore.LocalResponseContinuityStoreConfig{})
			orchestrator, endpoint := newFixtureResponsesOrchestrator(t, upstream, store)

			_, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
				EndpointName: endpoint.Name(),
				RequestID:    "req-missing-parent",
				Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
					Model:              "m",
					PreviousResponseID: "resp_missing_local",
					InputText:          "continue",
				}),
				Contract: requestpath.NewExecutionContract(deliveryMode),
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			var swobuErr compatibility.Error
			if !errors.As(err, &swobuErr) {
				t.Fatalf("error type = %T, want compatibility.Error", err)
			}
			if got := swobuErr.Code; got != compatibility.ErrorCodeBadRequest {
				t.Fatalf("error code = %q, want %q", got, compatibility.ErrorCodeBadRequest)
			}
			if !strings.Contains(swobuErr.Message, "previous_response_id could not be rehydrated") {
				t.Fatalf("error message = %q, want rehydration failure", swobuErr.Message)
			}
			if calls != 0 {
				t.Fatalf("upstream calls = %d, want 0", calls)
			}
		})
	}
}

func TestRequestPathHandler_RecoversFromUpstreamPreviousResponseNotFoundWhenLocalThreadExists(t *testing.T) {
	t.Parallel()

	errorFixture := mustReadResponsesContinuityFixtureApp(t, "openai_previous_response_not_found.json")
	successFixture := mustReadResponsesContinuityFixtureApp(t, "openrouter_buffered_ok.json")
	tests := []compatibility.DeliveryMode{
		compatibility.DeliveryModeBuffered,
		compatibility.DeliveryModeStreaming,
	}

	for _, deliveryMode := range tests {
		t.Run(string(deliveryMode), func(t *testing.T) {
			t.Parallel()

			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write(errorFixture)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(successFixture)
			}))
			defer upstream.Close()

			store := continuitystore.NewLocalResponseContinuityStore(continuitystore.LocalResponseContinuityStoreConfig{})
			namespace := compatibility.NewContinuationNamespace("alpha")
			if err := store.Store(context.Background(), namespace, compatibility.NewContinuitySnapshot(
				"resp_does_not_exist",
				"m",
				[]compatibility.CanonicalItem{
					compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
					compatibility.NewTextItem(compatibility.ItemAuthorAssistant, "ok"),
				},
			)); err != nil {
				t.Fatalf("seed Store returned error: %v", err)
			}
			orchestrator, endpoint := newFixtureResponsesOrchestrator(t, upstream, store)

			out, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
				EndpointName: endpoint.Name(),
				RequestID:    "req-upstream-missing-parent",
				Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
					Model:              "m",
					PreviousResponseID: "resp_does_not_exist",
					InputText:          "continue",
				}),
				Contract: requestpath.NewExecutionContract(deliveryMode),
			})
			if err != nil {
				t.Fatalf("HandleRequest returned error: %v", err)
			}
			if deliveryMode == compatibility.DeliveryModeStreaming {
				if err := drainOutputStream(out.Response); err != nil {
					t.Fatalf("drain recovered stream: %v", err)
				}
			} else {
				output, ok := out.Response.Output().(compatibility.CanonicalOutputValue)
				if !ok {
					t.Fatalf("output type = %T, want compatibility.CanonicalOutputValue", out.Response.Output())
				}
				if got := output.Text(); !strings.EqualFold(got, "ok") {
					t.Fatalf("output text = %q, want case-insensitive %q", got, "ok")
				}
			}
			if calls != 2 {
				t.Fatalf("upstream calls = %d, want 2", calls)
			}
		})
	}
}

func TestRequestPathHandler_RecordsContinuityRecoveryInEvidence(t *testing.T) {
	t.Parallel()

	errorFixture := mustReadResponsesContinuityFixtureApp(t, "openai_previous_response_not_found.json")
	successFixture := mustReadResponsesContinuityFixtureApp(t, "openrouter_buffered_ok.json")
	attempt := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(errorFixture)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(successFixture)
	}))
	defer upstream.Close()

	store := continuitystore.NewLocalResponseContinuityStore(continuitystore.LocalResponseContinuityStoreConfig{})
	if err := store.Store(context.Background(), compatibility.NewContinuationNamespace("alpha"), compatibility.NewContinuitySnapshot(
		"resp_does_not_exist",
		"m",
		[]compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
			compatibility.NewTextItem(compatibility.ItemAuthorAssistant, "ok"),
		},
	)); err != nil {
		t.Fatalf("seed Store returned error: %v", err)
	}
	evidence := &capturingEvidenceSink{}
	endpoint := testEndpointWithBaseURLAndProtocol(t, upstream.URL+"/v1", protocolsurface.Responses)
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := customadapter.NewExecutor(upstream.Client(), nil)
	orchestrator := requestpath.NewRequestHandler(reader, provider, evidence, store)

	_, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-recovered-parent",
		Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:              "m",
			PreviousResponseID: "resp_does_not_exist",
			InputText:          "continue",
		}),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("HandleRequest returned error: %v", err)
	}

	if len(evidence.events) != 2 {
		t.Fatalf("events = %d, want 2", len(evidence.events))
	}
	got := evidence.events[1]
	if got.AttemptCount() != 2 {
		t.Fatalf("attempt count = %d, want 2", got.AttemptCount())
	}
	if !got.ContinuityRecovered() {
		t.Fatal("expected continuity replay recovered evidence")
	}
	if got.ContinuityRecoveryTrigger() != "previous_response_not_found" {
		t.Fatalf("recovery trigger = %q, want %q", got.ContinuityRecoveryTrigger(), "previous_response_not_found")
	}
}

func TestRequestPathHandler_UsesFullThreadForToolBearingContinuityRecovery(t *testing.T) {
	t.Parallel()

	errorFixture := mustReadResponsesContinuityFixtureApp(t, "openai_previous_response_not_found.json")
	successFixture := mustReadResponsesContinuityFixtureApp(t, "openrouter_buffered_tool_call.json")
	var requestBodies []string
	attempt := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		requestBodies = append(requestBodies, string(raw))
		attempt++
		if attempt == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(errorFixture)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(successFixture)
	}))
	defer upstream.Close()

	store := continuitystore.NewLocalResponseContinuityStore(continuitystore.LocalResponseContinuityStoreConfig{})
	if err := store.Store(context.Background(), compatibility.NewContinuationNamespace("alpha"), compatibility.NewContinuitySnapshot(
		"resp_does_not_exist",
		"m",
		[]compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
		},
	)); err != nil {
		t.Fatalf("seed Store returned error: %v", err)
	}
	orchestrator, endpoint := newFixtureResponsesOrchestrator(t, upstream, store)

	out, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-tool-recovery",
		Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:              "m",
			PreviousResponseID: "resp_does_not_exist",
			Thread: []compatibility.CanonicalItem{
				compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
				compatibility.NewToolUseItem(compatibility.ItemAuthorAssistant, "", "call_1", "grep", map[string]any{"pattern": "TODO"}),
				compatibility.NewToolResultItem(compatibility.ItemAuthorTool, "call_1", "2 hits"),
				compatibility.NewTextItem(compatibility.ItemAuthorUser, "continue"),
			},
		}),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("HandleRequest returned error: %v", err)
	}

	if len(requestBodies) != 2 {
		t.Fatalf("upstream requests = %d, want 2", len(requestBodies))
	}
	if got := requestBodies[0]; got != `{"model":"m","input":[{"type":"function_call","call_id":"call_1","name":"grep","arguments":"{\"pattern\":\"TODO\"}"},{"type":"function_call_output","call_id":"call_1","output":"2 hits"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}],"previous_response_id":"resp_does_not_exist"}` {
		t.Fatalf("first upstream body = %q, want delta with tool-bearing suffix", got)
	}
	if got := requestBodies[1]; got != `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},{"type":"function_call","call_id":"call_1","name":"grep","arguments":"{\"pattern\":\"TODO\"}"},{"type":"function_call_output","call_id":"call_1","output":"2 hits"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}` {
		t.Fatalf("second upstream body = %q, want full thread with tool items", got)
	}

	output, ok := out.Response.Output().(compatibility.CanonicalOutputValue)
	if !ok {
		t.Fatalf("output type = %T, want compatibility.CanonicalOutputValue", out.Response.Output())
	}
	items := output.Items()
	if len(items) != 1 {
		t.Fatalf("output items = %d, want 1", len(items))
	}
	if got := items[0].Kind; got != compatibility.ItemKindToolUse {
		t.Fatalf("output item kind = %q, want %q", got, compatibility.ItemKindToolUse)
	}
	if got := items[0].Name; got != "grep" {
		t.Fatalf("tool name = %q, want %q", got, "grep")
	}
	if got := items[0].Input["pattern"]; got != "TODO" {
		t.Fatalf("tool input = %#v, want %q", got, "TODO")
	}
}

func newFixtureResponsesOrchestrator(
	t *testing.T,
	upstream *httptest.Server,
	store *continuitystore.LocalResponseContinuityStore,
) (requestpath.RequestHandler, endpointintent.Endpoint) {
	t.Helper()

	endpoint := testEndpointWithBaseURLAndProtocol(t, upstream.URL+"/v1", protocolsurface.Responses)
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := customadapter.NewExecutor(upstream.Client(), nil)
	return requestpath.NewRequestHandler(reader, provider, nil, store), endpoint
}

func testEndpointWithBaseURLAndProtocol(t *testing.T, baseURL string, protocolKind protocolsurface.Kind) endpointintent.Endpoint {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(
		ref,
		spec,
		baseURL,
		"",
		protocolKind,
	)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithModelID("m")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{providerConfig}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}

func drainOutputStream(resp any) error {
	execResp, ok := resp.(interface {
		Stream() compatibility.CanonicalOutputEventStream
		Close() error
	})
	if !ok {
		return compatibility.InternalError("streaming execute response is invalid")
	}
	defer func() { _ = execResp.Close() }()
	for {
		_, err := execResp.Stream().Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func mustReadResponsesContinuityFixtureApp(t *testing.T, name string) []byte {
	t.Helper()

	path := filepath.Join("..", "..", "fixtures", "responses_continuity", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}

func mustExtractResponsesSSEStartedIDApp(t *testing.T, fixture []byte) string {
	t.Helper()
	for _, line := range strings.Split(string(fixture), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var frame struct {
			Type     string `json:"type"`
			Response struct {
				ID string `json:"id"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			continue
		}
		if frame.Type == "response.created" && strings.TrimSpace(frame.Response.ID) != "" {
			return frame.Response.ID
		}
	}
	t.Fatal("responses sse fixture missing response.created id")
	return ""
}
