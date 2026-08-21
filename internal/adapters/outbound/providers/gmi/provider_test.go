package gmi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "gmi-token", nil
}

func TestRuntimeComposesSharedProtocolsAndGMIResponsesWebSearch(t *testing.T) {
	bundle := NewRuntime(http.DefaultClient, credentialResolver{})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, canonical.NewWebSearchDeclaration()), canonicaltest.Message(t, canonical.MessageRoleUser, "search")}})
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses, protocolkind.Messages} {
		target := provider.NewTargetSnapshot("gmi", "gmi", "https://gmi.example/v1", "env:GMI_API_KEY", kind, string(kind), delivery.BufferedDelivery())
		target.Model = request.Model()
		backend, err := bundle.BackendResolver.ResolveBackend(target)
		if err != nil {
			t.Fatal(err)
		}
		doc, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
		if kind == protocolkind.ChatCompletions || kind == protocolkind.Messages {
			if err == nil {
				t.Fatalf("GMI %s unexpectedly inherited hosted search", kind)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if kind == protocolkind.Responses {
			var payload map[string]any
			if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			tools, ok := payload["tools"].([]any)
			if !ok || len(tools) != 1 || tools[0].(map[string]any)["type"] != "web_search_preview" {
				t.Fatalf("GMI Responses tools = %#v", payload["tools"])
			}
		}
	}
}

func TestMessagesUsesGMICompatibilityAuthWhileOtherProtocolsRemainBearer(t *testing.T) {
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses, protocolkind.Messages} {
		t.Run(string(kind), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer gmi-token" {
					t.Fatalf("Authorization = %q", got)
				}
				if kind == protocolkind.Messages {
					if got := r.Header.Get("X-API-Key"); got != "gmi-token" {
						t.Fatalf("X-API-Key = %q", got)
					}
					if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
						t.Fatalf("anthropic-version = %q", got)
					}
				} else if got := r.Header.Get("X-API-Key"); got != "" {
					t.Fatalf("unexpected X-API-Key = %q", got)
				}
				w.Header().Set("Content-Type", "application/json")
				if kind == protocolkind.Messages {
					_, _ = w.Write([]byte(`{"id":"msg_1","model":"model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
					return
				}
				if kind == protocolkind.Responses {
					_, _ = w.Write([]byte(`{"id":"resp_1","model":"model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer srv.Close()
			bundle := NewRuntime(srv.Client(), credentialResolver{})
			target := provider.NewTargetSnapshot("gmi", "gmi", srv.URL+"/v1", "env:GMI_API_KEY", kind, string(kind), delivery.BufferedDelivery())
			target.Model = "model"
			backend, err := bundle.BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
			doc, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Transport.Send(context.Background(), doc); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestModelCatalogUsesBearerWithoutMessagesOnlyHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer gmi-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-API-Key"); got != "" {
			t.Fatalf("X-API-Key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "" {
			t.Fatalf("anthropic-version = %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"exact-model"}]}`))
	}))
	defer srv.Close()
	bundle := NewRuntime(srv.Client(), credentialResolver{})
	target := provider.NewTargetSnapshot("gmi", "gmi", srv.URL+"/v1", "env:GMI_API_KEY", protocolkind.Messages, "messages", delivery.BufferedDelivery())
	if _, err := bundle.Discovery.ProbeTarget(context.Background(), target); err != nil {
		t.Fatal(err)
	}
}

var _ providersruntime.Discovery = NewRuntime(nil, nil).Discovery
