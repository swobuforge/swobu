package opencodezen

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/thread"
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

func TestOpenCodeProjectsThreadAcrossBufferedAndStreamingRails(t *testing.T) {
	const rawClientSession = "secret-marker-123"
	threadID, err := thread.Derive("client/x-opencode-session/v1", "alpha", rawClientSession)
	if err != nil {
		t.Fatal(err)
	}
	otherThreadID, err := thread.Derive("client/x-opencode-session/v1", "alpha", "other-marker")
	if err != nil {
		t.Fatal(err)
	}
	wantSession, err := thread.Project("provider/opencode-session/v1", threadID)
	if err != nil {
		t.Fatal(err)
	}
	wantOtherSession, err := thread.Project("provider/opencode-session/v1", otherThreadID)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses, protocolkind.Messages} {
		for _, mode := range []delivery.Delivery{delivery.BufferedDelivery(), delivery.StreamingDelivery(delivery.FramingSSE)} {
			contract := string(kind)
			if mode.IsStreaming() {
				contract += "_stream"
			}
			t.Run(contract, func(t *testing.T) {
				var sessions []string
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					session := r.Header.Get("X-Opencode-Session")
					sessions = append(sessions, session)
					if session == rawClientSession {
						t.Fatal("raw client identity reached the OpenCode transport")
					}
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
					if mode.IsStreaming() {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = w.Write([]byte("data: [DONE]\n\n"))
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{}`))
				}))
				defer srv.Close()
				bundle := NewRuntime(srv.Client(), credentialResolver{})
				target := provider.NewTargetSnapshot("zen", string(profile.ProviderSpecOpenCodeZen), srv.URL+"/v1", "env:ZEN_API_KEY", kind, contract, mode)
				target.Model = "model"
				backend, err := bundle.BackendResolver.ResolveBackend(target)
				if err != nil {
					t.Fatal(err)
				}
				request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
				for _, id := range []thread.ID{threadID, threadID, otherThreadID} {
					doc, _, err := backend.Codec.Encode(provider.Request{Attempt: provider.AttemptContext{ThreadID: id}, Canonical: request, Delivery: mode})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := backend.Transport.Send(context.Background(), doc); err != nil {
						t.Fatal(err)
					}
				}
				if len(sessions) != 3 || sessions[0] != wantSession || sessions[1] != wantSession || sessions[2] != wantOtherSession {
					t.Fatalf("OpenCode session projections = %#v", sessions)
				}
			})
		}
	}
}

func TestOpenCodeTargetCharacterizationRecordReplayProjectsStableThread(t *testing.T) {
	var sessions []string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls++
		sessions = append(sessions, request.Header.Get("X-Opencode-Session"))
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"parallel_tool_calls rejected"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"response","model":"model","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	target := provider.NewTargetSnapshot("zen", string(profile.ProviderSpecOpenCodeZen), server.URL+"/v1", "env:ZEN_API_KEY", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
	target.Model = "model"
	backend, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	resolution := backend.CharacterizeTargetFact(context.Background(), provider.AcceptsParallelToolCallsFalse)
	if !resolution.Conclusive || resolution.Value {
		t.Fatalf("resolution = %#v, want conclusive false", resolution)
	}
	if len(sessions) != 2 || sessions[0] == "" || sessions[0] != sessions[1] {
		t.Fatalf("characterization sessions = %#v", sessions)
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
