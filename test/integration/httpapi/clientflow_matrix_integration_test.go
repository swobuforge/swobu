package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/websocket"

	"github.com/metrofun/swobu/internal/adapters/inbound/httpapi"
	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/ports"
)

func TestClientFlow_ExplicitClientProviderProtocolModelMatrix(t *testing.T) {
	rows := []struct {
		name           string
		client         string
		provider       string
		protocol       string
		model          string
		transport      string
		path           string
		clientUA       string
		contentType    string
		body           string
		extraHeaders   map[string]string
		expectProtocol string
		expectIngress  compatibility.IngressFamily
		expectNorm     compatibility.NormalizedPath
	}{
		{
			name:           "codex-openai-responses-primary-websocket",
			client:         "codex-cli",
			provider:       "openai",
			protocol:       "responses",
			model:          "primary",
			transport:      "websocket",
			path:           "/c/openai/responses",
			clientUA:       "Codex/0.121.0",
			body:           `{"type":"response.create","model":"primary","input":"hi","stream":true}`,
			expectProtocol: "openai_compat",
			expectIngress:  compatibility.IngressFamilyResponses,
			expectNorm:     compatibility.NormalizedPathResponses,
		},
		{
			name:           "codex-openai-chat-primary-http",
			client:         "codex-cli",
			provider:       "openai",
			protocol:       "chat_completions",
			model:          "primary",
			transport:      "http_post",
			path:           "/c/openai/v1/chat/completions",
			clientUA:       "Codex/0.121.0",
			contentType:    "application/json",
			body:           `{"model":"primary","messages":[{"role":"user","content":"hi"}]}`,
			expectProtocol: "openai_compat",
			expectIngress:  compatibility.IngressFamilyChatCompletions,
			expectNorm:     compatibility.NormalizedPathChatCompletions,
		},
		{
			name:           "continue-openrouter-chat-primary-http",
			client:         "continue",
			provider:       "openrouter",
			protocol:       "chat_completions",
			model:          "primary",
			transport:      "http_post",
			path:           "/c/openrouter/v1/chat/completions",
			clientUA:       "Continue/1.0",
			contentType:    "application/json",
			body:           `{"model":"primary","messages":[{"role":"user","content":"hi"}]}`,
			expectProtocol: "openai_compat",
			expectIngress:  compatibility.IngressFamilyChatCompletions,
			expectNorm:     compatibility.NormalizedPathChatCompletions,
		},
		{
			name:           "continue-openrouter-responses-primary-http",
			client:         "continue",
			provider:       "openrouter",
			protocol:       "responses",
			model:          "primary",
			transport:      "http_post",
			path:           "/c/openrouter/v1/responses",
			clientUA:       "Continue/1.0",
			contentType:    "application/json",
			body:           `{"model":"primary","input":"hi"}`,
			expectProtocol: "openai_compat",
			expectIngress:  compatibility.IngressFamilyResponses,
			expectNorm:     compatibility.NormalizedPathResponses,
		},
		{
			name:           "continue-openrouter-completions-primary-http",
			client:         "continue",
			provider:       "openrouter",
			protocol:       "completions",
			model:          "primary",
			transport:      "http_post",
			path:           "/c/openrouter/v1/completions",
			clientUA:       "Continue/1.0",
			contentType:    "application/json",
			body:           `{"model":"primary","prompt":"hi"}`,
			expectProtocol: "openai_compat",
			expectIngress:  compatibility.IngressFamilyCompletions,
			expectNorm:     compatibility.NormalizedPathCompletions,
		},
		{
			name:           "claude-anthropic-messages-primary-http",
			client:         "claude-code",
			provider:       "anthropic",
			protocol:       "messages",
			model:          "primary",
			transport:      "http_post",
			path:           "/c/anthropic/v1/messages",
			clientUA:       "Claude-Code/0.1.0",
			contentType:    "application/json",
			body:           `{"model":"primary","messages":[{"role":"user","content":"hi"}]}`,
			extraHeaders:   map[string]string{"anthropic-version": "2023-06-01"},
			expectProtocol: "anthropic_compat",
			expectIngress:  compatibility.IngressFamilyMessages,
			expectNorm:     compatibility.NormalizedPathMessages,
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			recorder := &matrixCapturingRequestHandler{
				out: outputForProtocol(row.protocol),
			}
			handler := httpapi.NewHandler(recorder)
			server := httptest.NewServer(handler)
			defer server.Close()

			if row.transport == "websocket" {
				wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + row.path
				cfg, err := websocket.NewConfig(wsURL, server.URL)
				if err != nil {
					t.Fatalf("NewConfig returned error: %v", err)
				}
				cfg.Header.Set("User-Agent", row.clientUA)
				conn, err := websocket.DialConfig(cfg)
				if err != nil {
					t.Fatalf("DialConfig returned error: %v", err)
				}
				defer func() {
					_ = conn.Close()
				}()
				if err := websocket.Message.Send(conn, row.body); err != nil {
					t.Fatalf("Send returned error: %v", err)
				}
				var joined strings.Builder
				for {
					var frame string
					if err := websocket.Message.Receive(conn, &frame); err != nil {
						t.Fatalf("Receive returned error: %v", err)
					}
					joined.WriteString(frame)
					if strings.Contains(frame, `"type":"response.completed"`) {
						break
					}
				}
				if !strings.Contains(joined.String(), `"type":"response.completed"`) {
					t.Fatalf("frames = %q, want response.completed", joined.String())
				}
			} else {
				req, err := http.NewRequest(http.MethodPost, server.URL+row.path, bytes.NewBufferString(row.body))
				if err != nil {
					t.Fatalf("NewRequest returned error: %v", err)
				}
				req.Header.Set("User-Agent", row.clientUA)
				if row.contentType != "" {
					req.Header.Set("Content-Type", row.contentType)
				}
				for key, value := range row.extraHeaders {
					req.Header.Set(key, value)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("Do returned error: %v", err)
				}
				defer func() {
					_ = resp.Body.Close()
				}()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
				}
			}

			got := recorder.got
			if got.Request == nil {
				t.Fatalf("compatibility_case client=%s provider=%s protocol=%s model=%s: request not captured", row.client, row.provider, row.protocol, row.model)
			}
			if gotModel := matrixRequestModel(got.Request); gotModel != row.model {
				t.Fatalf("compatibility_case client=%s provider=%s protocol=%s model=%s: captured model=%q", row.client, row.provider, row.protocol, row.model, gotModel)
			}
			if gotProvider := got.EndpointName.String(); gotProvider != row.provider {
				t.Fatalf("compatibility_case client=%s provider=%s protocol=%s model=%s: endpoint=%q", row.client, row.provider, row.protocol, row.model, gotProvider)
			}
			if gotProvenance := got.Provenance.ClientProtocol; gotProvenance != row.expectProtocol {
				t.Fatalf("compatibility_case client=%s provider=%s protocol=%s model=%s: client_protocol=%q", row.client, row.provider, row.protocol, row.model, gotProvenance)
			}
			if gotIngress := got.Provenance.IngressFamily; gotIngress != row.expectIngress {
				t.Fatalf("compatibility_case client=%s provider=%s protocol=%s model=%s: ingress_family=%q", row.client, row.provider, row.protocol, row.model, gotIngress)
			}
			if gotNorm := got.Provenance.NormalizedOp; gotNorm != row.expectNorm {
				t.Fatalf("compatibility_case client=%s provider=%s protocol=%s model=%s: normalized_op=%q", row.client, row.provider, row.protocol, row.model, gotNorm)
			}
		})
	}
}

type matrixCapturingRequestHandler struct {
	out requestpath.HandleOutput
	got requestpath.HandleInput
}

func (h *matrixCapturingRequestHandler) Handle(_ context.Context, in requestpath.HandleInput) (requestpath.HandleOutput, error) {
	h.got = in
	return h.out, nil
}

func matrixRequestModel(req compatibility.CanonicalRequest) string {
	switch typed := req.(type) {
	case compatibility.DialogCanonicalRequest:
		return typed.Model()
	case compatibility.GenerationCanonicalRequest:
		return typed.Model()
	case compatibility.PromptCanonicalRequest:
		return typed.Model()
	default:
		return ""
	}
}

func outputForProtocol(protocol string) requestpath.HandleOutput {
	switch protocol {
	case "responses":
		stream := compatibility.NewSliceEventStream([]compatibility.OutputEvent{
			{Kind: compatibility.OutputEventStarted, ResultID: "resp_1", Model: "primary"},
			{Kind: compatibility.OutputEventTextDelta, ItemID: "text_0", TextDelta: "ok"},
			{Kind: compatibility.OutputEventCompleted, FinishReason: "completed"},
		})
		return requestpath.HandleOutput{Response: ports.NewStreamingExecuteResponse(stream)}
	case "completions":
		return requestpath.HandleOutput{
			Response: ports.NewBufferedExecuteResponse(
				compatibility.NewPromptOutput("cmpl_1", "primary", []compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				}, "stop"),
			),
		}
	default:
		return requestpath.HandleOutput{
			Response: ports.NewBufferedExecuteResponse(
				compatibility.NewConversationOutput("resp_1", "primary", []compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				}, "stop"),
			),
		}
	}
}
