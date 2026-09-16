package vercel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "vercel-token", nil
}

func TestDiscoveryKeepsLanguageModelsWithAuthoritativeMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("catalog request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer vercel-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-API-Key"); got != "" {
			t.Fatalf("X-API-Key = %q, want absent", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "" {
			t.Fatalf("anthropic-version = %q, want absent", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"alibaba/qwen-3-14b","type":"language","name":"Qwen3-14B","owned_by":"alibaba"},{"id":"creator/embed-model","type":"embedding"},{"id":"creator/image-model","type":"image"},{"id":"creator/video-model","type":"video"},{"id":"creator/unknown-model","type":"future"},{"id":"creator/missing-type"}]}`))
	}))
	defer server.Close()

	probe := probeModels(t, server)
	if len(probe.Options) != 1 {
		t.Fatalf("model options = %#v", probe.Options)
	}
	option := probe.Options[0]
	if option.Name != "alibaba/qwen-3-14b" || option.ModelName != "alibaba/qwen-3-14b" || option.ModelPublisher != "alibaba" || option.Family != "" {
		t.Fatalf("language model metadata = %#v", option)
	}
}

func TestDiscoveryIgnoresRowsWithUnprovenLanguageType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"creator/valid","type":"language"},{"id":"creator/number","type":123},{"id":"creator/object","type":{}},{"id":"creator/null","type":null},{"id":"creator/unknown","type":"future"},{"id":"creator/missing"}]}`))
	}))
	defer server.Close()

	probe := probeModels(t, server)
	if len(probe.Options) != 1 || probe.Options[0].Name != "creator/valid" {
		t.Fatalf("model options = %#v", probe.Options)
	}
}

func TestDiscoveryKeepsLanguageRowsWithMalformedOptionalMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"creator/missing","type":"language"},{"id":"creator/null","type":"language","name":null,"owned_by":null},{"id":"creator/non-string","type":"language","name":123,"owned_by":{}}]}`))
	}))
	defer server.Close()

	probe := probeModels(t, server)
	if len(probe.Options) != 3 {
		t.Fatalf("model options = %#v", probe.Options)
	}
	for _, option := range probe.Options {
		if option.ModelName != option.Name || option.ModelPublisher != "" || option.Family != "" {
			t.Fatalf("optional metadata was fabricated or routable identity changed: %#v", option)
		}
	}
}

func probeModels(t *testing.T, server *httptest.Server) provider.TargetProbeResult {
	t.Helper()
	bundle := NewRuntime(server.Client(), credentialResolver{})
	target := provider.NewTargetSnapshot("draft", string(profile.ProviderSpecVercel), server.URL+"/v1", "env:AI_GATEWAY_API_KEY", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	probe, err := bundle.Discovery.ProbeTarget(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	return probe
}

func TestRuntimeUsesStandardPathsWithBearerAndMessagesVersionHeader(t *testing.T) {
	tests := []struct {
		kind protocolkind.ProtocolKind
		path string
		body string
	}{
		{kind: protocolkind.Responses, path: "/v1/responses", body: `{"id":"resp_1","model":"creator/model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`},
		{kind: protocolkind.ChatCompletions, path: "/v1/chat/completions", body: `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`},
		{kind: protocolkind.Messages, path: "/v1/messages", body: `{"id":"msg_1","model":"creator/model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != test.path {
					t.Fatalf("inference request = %s %s, want POST %s", r.Method, r.URL.Path, test.path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer vercel-token" {
					t.Fatalf("Authorization = %q", got)
				}
				wantVersion := ""
				if test.kind == protocolkind.Messages {
					wantVersion = "2023-06-01"
				}
				if got := r.Header.Get("anthropic-version"); got != wantVersion {
					t.Fatalf("anthropic-version = %q, want %q", got, wantVersion)
				}
				if got := r.Header.Get("X-API-Key"); got != "" {
					t.Fatalf("X-API-Key = %q, want absent", got)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			bundle := NewRuntime(server.Client(), credentialResolver{})
			target := provider.NewTargetSnapshot("vercel", string(profile.ProviderSpecVercel), server.URL+"/v1", "env:AI_GATEWAY_API_KEY", test.kind, string(test.kind), delivery.BufferedDelivery())
			target.Model = "creator/model"
			backend, err := bundle.BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("creator/model"),
				Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
			})
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

func TestProjectModelRequiresExactLanguageType(t *testing.T) {
	for _, rawType := range []string{"Language", " language ", "LANGUAGE"} {
		t.Run(strings.ReplaceAll(rawType, " ", "_"), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"data":[{"id":"creator/model","type":"` + rawType + `"}]}`))
			}))
			defer server.Close()
			bundle := NewRuntime(server.Client(), credentialResolver{})
			target := provider.NewTargetSnapshot("draft", string(profile.ProviderSpecVercel), server.URL+"/v1", "env:AI_GATEWAY_API_KEY", protocolkind.Responses, "responses", delivery.BufferedDelivery())
			probe, err := bundle.Discovery.ProbeTarget(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			if len(probe.Options) != 0 {
				t.Fatalf("non-exact language type produced options: %#v", probe.Options)
			}
		})
	}
}
