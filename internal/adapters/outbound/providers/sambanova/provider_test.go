package sambanova

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "sambanova-token", nil
}

func TestRuntimeComposesAllSharedProtocolsAndMessagesRefinement(t *testing.T) {
	bundle := NewRuntime(http.DefaultClient, credentialResolver{})
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses, protocolkind.Messages} {
		backend, err := bundle.BackendResolver.ResolveBackend(sambaNovaTarget("https://api.sambanova.ai/v1", kind))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if kind == protocolkind.Messages {
			if _, ok := backend.Codec.(messagesCodec); !ok {
				t.Fatalf("Messages codec = %T", backend.Codec)
			}
		}
	}
}

func TestTransportAndDiscoveryUseEffectiveEndpointAndBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sambanova-token" {
			t.Fatalf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/stack/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		case "/stack/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"served-model"}]}`))
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()
	target := sambaNovaTarget(server.URL+"/stack/v1", protocolkind.ChatCompletions)
	bundle := NewRuntime(server.Client(), credentialResolver{})
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
	if len(result.Deployments) != 1 || result.Deployments[0].Name != "served-model" {
		t.Fatalf("deployments = %#v", result.Deployments)
	}
}

func TestTransportUsesEachSelectedSharedProtocolPath(t *testing.T) {
	for _, tc := range []struct {
		kind           protocolkind.ProtocolKind
		path, response string
	}{
		{protocolkind.ChatCompletions, "/v1/chat/completions", `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`},
		{protocolkind.Responses, "/v1/responses", `{"id":"resp_1","model":"served-model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`},
		{protocolkind.Messages, "/v1/messages", `{"id":"msg_1","model":"served-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path || r.Header.Get("Authorization") != "Bearer sambanova-token" {
					t.Fatalf("path/auth = %s/%q, want %s/Bearer", r.URL.Path, r.Header.Get("Authorization"), tc.path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			backend, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(sambaNovaTarget(server.URL+"/v1", tc.kind))
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
		})
	}
}

func TestMessagesThinkingOnlyOmitsDocumentedUnsupportedForms(t *testing.T) {
	budget, err := canonical.NewBudgetReasoningCompute(128)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name         string
		compute      canonical.Specified[canonical.ReasoningCompute]
		wantThinking string
		wantChange   bool
	}{
		{name: "absent", wantThinking: ""},
		{name: "disabled", compute: canonical.Specify(canonical.NewDisabledReasoningCompute()), wantThinking: "disabled"},
		{name: "adaptive", compute: canonical.Specify(canonical.NewAutomaticReasoningCompute()), wantChange: true},
		{name: "enabled", compute: canonical.Specify(budget), wantChange: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: tc.compute})
			if err != nil {
				t.Fatal(err)
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model-name-does-not-matter"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}, Reasoning: reasoning})
			document, changes, err := (messagesCodec{}).Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			payload := sambaNovaPayload(t, document.RawBytes())
			thinking, present := payload["thinking"].(map[string]any)
			if tc.wantThinking == "" {
				if present {
					t.Fatalf("thinking = %#v, want absent", thinking)
				}
			} else if !present || thinking["type"] != tc.wantThinking {
				t.Fatalf("thinking = %#v, want %q", thinking, tc.wantThinking)
			}
			if payload["model"] != "model-name-does-not-matter" || payload["stream"] != true {
				t.Fatalf("unrelated fields changed: %#v", payload)
			}
			if tc.wantChange {
				if len(changes) != 1 || changes[0].Capability != canonical.RequestReasoning || changes[0].Kind != compat.Approximation || changes[0].Preserved != canonical.RequestReasoning {
					t.Fatalf("changes = %#v", changes)
				}
			} else if len(changes) != 0 {
				t.Fatalf("changes = %#v", changes)
			}
		})
	}
}

func sambaNovaTarget(baseURL string, kind protocolkind.ProtocolKind) provider.TargetSnapshot {
	protocol := string(kind)
	if kind == protocolkind.ChatCompletions {
		protocol = "chat_completions_stream"
	}
	if kind == protocolkind.Responses {
		protocol = "responses_stream"
	}
	if kind == protocolkind.Messages {
		protocol = "messages_stream"
	}
	target := provider.NewTargetSnapshot("sambanova", "sambanova", baseURL, "env:SAMBANOVA_API_KEY", kind, profile.FrameSSEEvent, protocol)
	target.Model = "served-model"
	return target
}

func sambaNovaPayload(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

var _ providersruntime.Discovery = NewRuntime(nil, nil).Discovery
