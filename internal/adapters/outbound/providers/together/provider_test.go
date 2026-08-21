package together

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "together-token", nil
}

func TestRuntimeUsesDerivedChatCompletionsOnly(t *testing.T) {
	bundle := NewRuntime(http.DefaultClient, credentialResolver{})
	target := provider.NewTargetSnapshot("together", "together", "https://api.together.ai/v1", "env:TOGETHER_API_KEY", protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = "future-model"
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	if codec, ok := backend.Codec.(protocolcodec.Codec); !ok || codec.ChatDialect.ResponseReasoning == nil {
		t.Fatalf("codec = %T, want Together reasoning dialect codec", backend.Codec)
	}
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.Responses, protocolkind.Messages} {
		invalid := target.Clone()
		invalid.ProtocolKind = kind
		if _, err := bundle.BackendResolver.ResolveBackend(invalid); err == nil {
			t.Fatalf("%s must not resolve as a Together backend", kind)
		}
	}
}

func TestTransportUsesFixedChatPathAndBearerCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer together-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	target := togetherTarget(server.URL + "/v1")
	backend, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
	})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryMergesDedicatedBeforeServerlessAndUsesEndpointName(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer together-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"image","type":"image"},{"id":"z-language","type":"language","display_name":"Z language","organization":"org"},{"id":"a-chat","type":"chat"},{"id":"embed","type":"embedding"}]}`))
		case "/v1/endpoints":
			if r.URL.RawQuery != "type=dedicated&mine=true" {
				t.Fatalf("endpoint query = %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"endpoint-ignored","name":"team/dedicated-a","model":"Qwen"},{"id":"endpoint-ignored-2","name":"team/dedicated-b","model":"Llama"}]}`))
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	result, err := NewRuntime(server.Client(), credentialResolver{}).Discovery.ProbeTarget(context.Background(), togetherTarget(server.URL+"/v1"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, deployment := range result.Options {
		names = append(names, deployment.Name)
	}
	want := []string{"team/dedicated-a", "team/dedicated-b", "a-chat", "z-language"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("deployments = %v, want %v", names, want)
	}
	if !reflect.DeepEqual(requests, []string{"/v1/models", "/v1/endpoints?type=dedicated&mine=true"}) {
		t.Fatalf("requests = %v", requests)
	}
}

func TestDiscoveryUsesPartialCatalogWithoutInventingACombinedFailureType(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		modelsStatus, endpointsStatus int
		want                          []string
	}{
		{name: "serverless only", endpointsStatus: http.StatusBadGateway, want: []string{"chat"}},
		{name: "dedicated only", modelsStatus: http.StatusBadGateway, want: []string{"team/dedicated"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/models":
					if tc.modelsStatus != 0 {
						w.WriteHeader(tc.modelsStatus)
						return
					}
					_, _ = w.Write([]byte(`{"data":[{"id":"chat","type":"chat"}]}`))
				case "/v1/endpoints":
					if tc.endpointsStatus != 0 {
						w.WriteHeader(tc.endpointsStatus)
						return
					}
					_, _ = w.Write([]byte(`{"data":[{"name":"team/dedicated"}]}`))
				}
			}))
			defer server.Close()
			result, err := NewRuntime(server.Client(), credentialResolver{}).Discovery.ProbeTarget(context.Background(), togetherTarget(server.URL+"/v1"))
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, deployment := range result.Options {
				names = append(names, deployment.Name)
			}
			if !reflect.DeepEqual(names, tc.want) {
				t.Fatalf("deployments = %v, want %v", names, tc.want)
			}
		})
	}
}

func TestReasoningCodecProjectsTogetherDialectWithoutPreservedThinking(t *testing.T) {
	bundle := NewRuntime(http.DefaultClient, credentialResolver{})
	target := togetherTarget("https://api.together.ai/v1")
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}

	effort := canonical.InferenceEffortHigh
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	automatic, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(canonical.NewAutomaticReasoningCompute())})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("future-model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}, Controls: controls, Reasoning: automatic})
	document, changes, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
	requestPayload := payload(t, document.RawBytes())
	reasoning, ok := requestPayload["reasoning"].(map[string]any)
	if !ok || reasoning["enabled"] != true || requestPayload["reasoning_effort"] != "high" {
		t.Fatalf("Together reasoning payload = %#v", requestPayload)
	}
	if _, has := requestPayload["clear_thinking"]; has {
		t.Fatalf("preserved thinking leaked into P0 request: %#v", requestPayload)
	}

	disabled := canonical.NewDisabledReasoningCompute()
	disabledReasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(disabled)})
	if err != nil {
		t.Fatal(err)
	}
	disabledRequest := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("future-model"), Reasoning: disabledReasoning})
	document, _, err = backend.Codec.Encode(provider.Request{Canonical: disabledRequest, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	requestPayload = payload(t, document.RawBytes())
	reasoning, _ = requestPayload["reasoning"].(map[string]any)
	if reasoning["enabled"] != false {
		t.Fatalf("disabled reasoning = %#v", requestPayload)
	}
}

func TestReasoningCodecCapturesReadableReasoningOnly(t *testing.T) {
	document := carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(`{"choices":[{"message":{"role":"assistant","reasoning":"think first","reasoning_content":"replay state","content":"answer"},"finish_reason":"stop"}]}`), carrier.Meta{})
	cleaned, item, err := protocolcodec.ExtractChatReasoningDocument(document, togetherChatReasoningExtractor{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned.RawBytes(), []byte(`"reasoning"`)) || !bytes.Contains(cleaned.RawBytes(), []byte("reasoning_content")) {
		t.Fatalf("cleaned response = %s", cleaned.RawBytes())
	}
	reasoning, ok := item.Reasoning()
	if !ok || !reasoning.Opaque().IsZero() {
		t.Fatalf("reasoning item = %#v", item)
	}

	stream := "data: {\"choices\":[{\"delta\":{\"reasoning\":\"think \"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{\"reasoning\":\"now\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"
	body := protocolcodec.NewChatReasoningSSEBody(io.NopCloser(strings.NewReader(stream)), togetherChatReasoningExtractor{})
	cleanedStream, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleanedStream, []byte(`"reasoning"`)) {
		t.Fatalf("cleaned stream = %s", cleanedStream)
	}
	item, ok = body.Take()
	if !ok {
		t.Fatal("streamed reasoning item missing")
	}
	reasoning, _ = item.Reasoning()
	if !reasoning.Opaque().IsZero() {
		t.Fatal("readable Together reasoning must not become replay state")
	}
}

func togetherTarget(baseURL string) provider.TargetSnapshot {
	target := provider.NewTargetSnapshot("together", "together", baseURL, "env:TOGETHER_API_KEY", protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = "future-model"
	return target
}

func payload(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

var _ providersruntime.Discovery = NewRuntime(nil, nil).Discovery
