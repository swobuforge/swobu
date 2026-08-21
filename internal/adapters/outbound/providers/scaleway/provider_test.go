package scaleway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	return "scaleway-token", nil
}

func TestRuntimeAdmitsOnlyScalewayChatAndResponses(t *testing.T) {
	bundle := NewRuntime(http.DefaultClient, credentialResolver{})
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses} {
		backend, err := bundle.BackendResolver.ResolveBackend(scalewayTarget("https://api.scaleway.ai/v1", "env:SCW_SECRET_KEY", kind))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if kind == protocolkind.ChatCompletions {
			if codec, ok := backend.Codec.(protocolcodec.Codec); !ok || codec.ChatDialect.ResponseReasoning == nil {
				t.Fatalf("Chat codec = %T", backend.Codec)
			}
		}
		if kind == protocolkind.Responses {
			if codec, ok := backend.Codec.(protocolcodec.Codec); !ok || !codec.ResponsesDialect.PrependInstructionsToInput {
				t.Fatalf("Responses codec = %T", backend.Codec)
			}
		}
	}
	if _, err := bundle.BackendResolver.ResolveBackend(scalewayTarget("https://api.scaleway.ai/v1", "", protocolkind.Messages)); err == nil {
		t.Fatal("Messages resolved for Scaleway")
	}
}

func TestTransportAndDiscoveryUseEffectiveEndpointWithOptionalBearer(t *testing.T) {
	for _, tc := range []struct{ name, credential, wantAuth string }{
		{name: "serverless credential", credential: "env:SCW_SECRET_KEY", wantAuth: "Bearer scaleway-token"},
		{name: "auth disabled dedicated", credential: "", wantAuth: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != tc.wantAuth {
					t.Fatalf("Authorization = %q, want %q", got, tc.wantAuth)
				}
				switch r.URL.Path {
				case "/v1/chat/completions":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
				case "/v1/models":
					_, _ = w.Write([]byte(`{"data":[{"id":"served-model"}]}`))
				default:
					t.Fatalf("path = %q", r.URL.Path)
				}
			}))
			defer server.Close()
			bundle := NewRuntime(server.Client(), credentialResolver{})
			target := scalewayTarget(server.URL+"/v1", tc.credential, protocolkind.ChatCompletions)
			backend, err := bundle.BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("served-model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Transport.Send(context.Background(), document); err != nil {
				t.Fatal(err)
			}
			result, err := bundle.Discovery.ProbeTarget(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Options) != 1 || result.Options[0].Name != "served-model" {
				t.Fatalf("deployments = %#v", result.Options)
			}
		})
	}
}

func TestResponsesRehomesInstructionsAndOmitsOnlyStoreFalse(t *testing.T) {
	bundle := NewRuntime(nil, credentialResolver{})
	backend, err := bundle.BackendResolver.ResolveBackend(scalewayTarget("https://api.scaleway.ai/v1", "env:SCW_SECRET_KEY", protocolkind.Responses))
	if err != nil {
		t.Fatal(err)
	}
	storeFalse := canonical.Specify(false)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("glm-5.2"),
		Items: []canonical.CanonicalItem{canonicaltest.MustInstruction(canonical.MessageRoleSystem, "be precise"), canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		Store: storeFalse,
	})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	payload := scalewayPayload(t, document.RawBytes())
	if _, ok := payload["instructions"]; ok {
		t.Fatalf("instructions leaked: %#v", payload)
	}
	if _, ok := payload["store"]; ok {
		t.Fatalf("store:false leaked: %#v", payload)
	}
	if _, ok := payload["previous_response_id"]; ok {
		t.Fatalf("stateless request carried continuation: %#v", payload)
	}
	if _, ok := payload["include"]; ok {
		t.Fatalf("Scaleway-rejected include leaked: %#v", payload)
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) != 2 {
		t.Fatalf("input = %#v", payload["input"])
	}
	if input[0].(map[string]any)["role"] != "system" || input[1].(map[string]any)["role"] != "user" {
		t.Fatalf("input roles = %#v", input)
	}
	if got := input[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]; got != "be precise" {
		t.Fatalf("system text = %#v", got)
	}
	if got := input[1].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]; got != "hello" {
		t.Fatalf("user text = %#v", got)
	}

	storeTrue := canonical.Specify(true)
	trueRequest := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("glm-5.2"), Items: request.Items(), Store: storeTrue})
	document, _, err = backend.Codec.Encode(provider.Request{Canonical: trueRequest, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if got := scalewayPayload(t, document.RawBytes())["store"]; got != true {
		t.Fatalf("store = %#v, want true", got)
	}
}

func TestReasoningCodecPreservesReadableFieldsWithoutOpaqueState(t *testing.T) {
	document := carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(`{"choices":[{"message":{"role":"assistant","reasoning_content":"first ","reasoning":"second","content":"answer"},"finish_reason":"stop"}]}`), carrier.Meta{})
	cleaned, item, err := protocolcodec.ExtractChatReasoningDocument(document, scalewayChatReasoningExtractor{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned.RawBytes(), []byte("reasoning")) {
		t.Fatalf("cleaned response = %s", cleaned.RawBytes())
	}
	reasoning, ok := item.Reasoning()
	if !ok || !reasoning.Opaque().IsZero() {
		t.Fatalf("reasoning = %#v", item)
	}

	stream := "data: {\"choices\":[{\"delta\":{\"reasoning\":\"think \"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{\"reasoning_content\":\"now\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"
	body := protocolcodec.NewChatReasoningSSEBody(io.NopCloser(strings.NewReader(stream)), scalewayChatReasoningExtractor{})
	cleanedStream, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleanedStream, []byte("reasoning")) {
		t.Fatalf("cleaned stream = %s", cleanedStream)
	}
	item, ok = body.Take()
	if !ok {
		t.Fatal("streamed reasoning missing")
	}
	reasoning, _ = item.Reasoning()
	if !reasoning.Opaque().IsZero() {
		t.Fatal("readable reasoning became opaque state")
	}
}

func TestChatKeepsStandardReasoningEffort(t *testing.T) {
	bundle := NewRuntime(nil, credentialResolver{})
	backend, err := bundle.BackendResolver.ResolveBackend(scalewayTarget("https://api.scaleway.ai/v1", "env:SCW_SECRET_KEY", protocolkind.ChatCompletions))
	if err != nil {
		t.Fatal(err)
	}
	effort := canonical.InferenceEffortHigh
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:    canonical.Specify("served-model"),
		Items:    []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		Controls: controls,
	})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if got := scalewayPayload(t, document.RawBytes())["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got)
	}
}

func scalewayTarget(baseURL, credential string, kind protocolkind.ProtocolKind) provider.TargetSnapshot {
	protocol := "chat_completions_stream"
	if kind == protocolkind.Responses {
		protocol = "responses_stream"
	}
	target := provider.NewTargetSnapshot("scaleway", "scaleway", baseURL, credential, kind, protocol, delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = "served-model"
	return target
}
func scalewayPayload(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

var _ providersruntime.Discovery = NewRuntime(nil, nil).Discovery
