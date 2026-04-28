package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpapi "github.com/metrofun/swobu/internal/adapters/inbound/httpapi"
	"github.com/metrofun/swobu/internal/adapters/outbound/continuitystore"
	customadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/custom"
	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/domain/runtimeevidence"
	"github.com/metrofun/swobu/internal/ports"
)

func TestHandler_ResponsesBufferedContinuityRecovery(t *testing.T) {
	t.Parallel()

	errorFixture := mustReadContinuityFixture(t, "openai_previous_response_not_found.json")
	successFixture := mustReadContinuityFixture(t, "openrouter_buffered_ok.json")
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		if upstreamCalls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(errorFixture)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(successFixture)
	}))
	defer upstream.Close()

	evidence := &capturingEvidenceSink{}
	handler := newFixtureHandler(t, upstream, protocolsurface.Responses, seedContinuityStore(t), evidence)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","previous_response_id":"resp_does_not_exist","input":"continue"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode body json: %v body=%q", err, body)
	}
	outputText, _ := payload["output_text"].(string)
	if !strings.EqualFold(strings.TrimSpace(outputText), "ok") {
		t.Fatalf("body = %q, want output_text semantically equal to ok", body)
	}
	if strings.Contains(body, `previous_response_not_found`) {
		t.Fatalf("body = %q, want recovered success, not backend miss", body)
	}
	if upstreamCalls != 2 {
		t.Fatalf("upstream calls = %d, want 2", upstreamCalls)
	}
	if len(evidence.events) != 2 {
		t.Fatalf("events = %d, want 2", len(evidence.events))
	}
	if got := evidence.events[1].Result(); got != runtimeevidence.ResultClassSuccess {
		t.Fatalf("terminal result = %q, want %q", got, runtimeevidence.ResultClassSuccess)
	}
	if got := evidence.events[1].AttemptCount(); got != 2 {
		t.Fatalf("attempt count = %d, want %d", got, 2)
	}
	if !evidence.events[1].ContinuityRecovered() {
		t.Fatal("expected continuity recovered evidence")
	}
}

func TestHandler_ResponsesStreamingContinuityRecovery(t *testing.T) {
	t.Parallel()

	errorFixture := mustReadContinuityFixture(t, "openai_previous_response_not_found.json")
	successFixture := mustReadContinuityFixture(t, "openrouter_stream_ok.sse")
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		if upstreamCalls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(errorFixture)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(successFixture)
	}))
	defer upstream.Close()

	handler := newFixtureHandler(t, upstream, protocolsurface.Responses, seedContinuityStore(t), &capturingEvidenceSink{})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","previous_response_id":"resp_does_not_exist","input":"continue","stream":true}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"response.created"`) {
		t.Fatalf("body = %q, want responses created event", body)
	}
	if !strings.Contains(body, `"type":"response.output_text.delta"`) {
		t.Fatalf("body = %q, want text delta event", body)
	}
	if strings.Contains(body, `"sequence_number"`) || strings.Contains(body, `: OPENROUTER PROCESSING`) {
		t.Fatalf("body = %q, want canonical client stream, not upstream frames", body)
	}
	if upstreamCalls != 2 {
		t.Fatalf("upstream calls = %d, want 2", upstreamCalls)
	}
}

func TestHandler_ResponsesMissingStoredParentFailsBeforeUpstream(t *testing.T) {
	t.Parallel()

	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		t.Fatalf("unexpected upstream call: %s %s", r.Method, r.URL.Path)
	}))
	defer upstream.Close()

	handler := newFixtureHandler(t, upstream, protocolsurface.Responses, continuitystore.NewLocalResponseContinuityStore(continuitystore.LocalResponseContinuityStoreConfig{}), &capturingEvidenceSink{})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","previous_response_id":"resp_missing_local","input":"continue"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `BAD_REQUEST`) || !strings.Contains(body, `previous_response_id could not be rehydrated`) {
		t.Fatalf("body = %q, want explicit local missing-parent failure", body)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls)
	}
}

func TestHandler_ChatCompletionsOntoResponsesTargetKeepsChatShape(t *testing.T) {
	t.Parallel()

	bufferedFixture := mustReadContinuityFixture(t, "openrouter_buffered_ok.json")
	streamFixture := mustReadContinuityFixture(t, "openrouter_stream_ok.sse")
	tests := []struct {
		name        string
		body        string
		fixture     []byte
		contentType string
		want        string
		notWant     string
	}{
		{
			name:        "buffered",
			body:        `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
			fixture:     bufferedFixture,
			contentType: "application/json",
			want:        `"object":"chat.completion"`,
			notWant:     `"object":"response"`,
		},
		{
			name:        "streaming",
			body:        `{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true}`,
			fixture:     streamFixture,
			contentType: "text/event-stream",
			want:        `"object":"chat.completion.chunk"`,
			notWant:     `"type":"response.created"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				_, _ = w.Write(tt.fixture)
			}))
			defer upstream.Close()

			handler := newFixtureHandler(t, upstream, protocolsurface.Responses, continuitystore.NewLocalResponseContinuityStore(continuitystore.LocalResponseContinuityStoreConfig{}), &capturingEvidenceSink{})
			req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tt.want) {
				t.Fatalf("body = %q, want %q", body, tt.want)
			}
			if strings.Contains(body, tt.notWant) {
				t.Fatalf("body = %q, must not contain %q", body, tt.notWant)
			}
		})
	}
}

func newFixtureHandler(
	t *testing.T,
	upstream *httptest.Server,
	protocolKind protocolsurface.Kind,
	store ports.ResponseContinuityStore,
	evidence ports.RequestEvidenceSink,
) http.Handler {
	t.Helper()

	endpoint := testEndpointWithBaseURLAndProtocol(t, upstream.URL+"/v1", protocolKind)
	reader := fixtureEndpointReader{endpoint: endpoint}
	provider := customadapter.NewExecutor(upstream.Client(), nil)
	return httpapi.NewHandler(requestpath.NewRequestHandler(reader, provider, evidence, store))
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

type fixtureEndpointReader struct {
	endpoint endpointintent.Endpoint
}

func (r fixtureEndpointReader) GetEndpoint(context.Context, endpointintent.EndpointName) (endpointintent.Endpoint, error) {
	return r.endpoint, nil
}

type capturingEvidenceSink struct {
	events []runtimeevidence.TrafficEvent
}

func (s *capturingEvidenceSink) Append(_ context.Context, event runtimeevidence.TrafficEvent) {
	s.events = append(s.events, event)
}

func seedContinuityStore(t *testing.T) ports.ResponseContinuityStore {
	t.Helper()

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
	return store
}

func mustReadContinuityFixture(t *testing.T, name string) []byte {
	t.Helper()

	// Error-shape truth comes from the typed upstream error fixture, while the
	// success fixtures are just real OpenAI-compatible payload shapes replayed offline.
	path := filepath.Join("..", "..", "fixtures", "responses_continuity", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}
