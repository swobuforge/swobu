package opencodezen

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "zen-token", nil
}

func TestProjectModelSelectsExactZenProtocol(t *testing.T) {
	tests := []struct {
		name    string
		row     string
		want    []string
		include bool
	}{
		{name: "Ox Chat", row: `{"id":"x-preview-f-free"}`, want: []string{"chat_completions", "chat_completions_stream"}, include: true},
		{name: "GPT Responses", row: `{"id":"gpt-5.6-sol"}`, want: []string{"responses", "responses_stream"}, include: true},
		{name: "Grok Responses", row: `{"id":"grok-code-fast-1"}`, want: []string{"responses", "responses_stream"}, include: true},
		{name: "Muse Responses", row: `{"id":"muse-1"}`, want: []string{"responses", "responses_stream"}, include: true},
		{name: "Claude Messages", row: `{"id":"claude-sonnet-4-6"}`, want: []string{"messages", "messages_stream"}, include: true},
		{name: "Qwen Messages", row: `{"id":"qwen3-coder"}`, want: []string{"messages", "messages_stream"}, include: true},
		{name: "DeepSeek Chat", row: `{"id":"deepseek-v3.2"}`, want: []string{"chat_completions", "chat_completions_stream"}, include: true},
		{name: "GLM Chat", row: `{"id":"glm-5"}`, want: []string{"chat_completions", "chat_completions_stream"}, include: true},
		{name: "MiniMax Chat", row: `{"id":"minimax-m2.5"}`, want: []string{"chat_completions", "chat_completions_stream"}, include: true},
		{name: "Kimi Chat", row: `{"id":"kimi-k2.5"}`, want: []string{"chat_completions", "chat_completions_stream"}, include: true},
		{name: "unsupported Google transport", row: `{"id":"gemini-3-pro"}`, include: false},
		{name: "unknown transport", row: `{"id":"future-model"}`, include: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows, err := modelcatalogopenai.DecodeModelRows(strings.NewReader(`{"data":[` + test.row + `]}`))
			if err != nil {
				t.Fatal(err)
			}
			option, include, err := projectModel(profile.ProviderSpecOpenCodeZen, rows[0])
			if err != nil {
				t.Fatal(err)
			}
			if include != test.include {
				t.Fatalf("include = %v, want %v", include, test.include)
			}
			if !include {
				return
			}
			if got := option.SupportedProviderProtocols; !equalStrings(got, test.want) {
				t.Fatalf("protocols = %v, want %v", got, test.want)
			}
			if option.DefaultProviderProtocol != test.want[0] {
				t.Fatalf("default = %q, want %q", option.DefaultProviderProtocol, test.want[0])
			}
		})
	}
}

func TestMessagesUsesZenNativeHeadersWhileOtherProtocolsRemainBearer(t *testing.T) {
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses, protocolkind.Messages} {
		t.Run(string(kind), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer zen-token" {
					t.Fatalf("Authorization = %q", got)
				}
				if kind == protocolkind.Messages {
					if got := r.Header.Get("X-API-Key"); got != "zen-token" {
						t.Fatalf("X-API-Key = %q", got)
					}
					if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
						t.Fatalf("anthropic-version = %q", got)
					}
				} else if got := r.Header.Get("X-API-Key"); got != "" {
					t.Fatalf("unexpected X-API-Key = %q", got)
				}
				w.Header().Set("Content-Type", "application/json")
				switch kind {
				case protocolkind.Messages:
					_, _ = w.Write([]byte(`{"id":"msg_1","model":"model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
				case protocolkind.Responses:
					_, _ = w.Write([]byte(`{"id":"resp_1","model":"model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
				default:
					_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
				}
			}))
			defer srv.Close()
			bundle := NewRuntime(srv.Client(), credentialResolver{})
			target := provider.NewTargetSnapshot("zen", string(profile.ProviderSpecOpenCodeZen), srv.URL+"/v1", "env:ZEN_API_KEY", kind, string(kind), delivery.BufferedDelivery())
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

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
