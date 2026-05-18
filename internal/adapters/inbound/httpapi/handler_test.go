package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/websocket"

	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/ports"
)

func TestHandler_PropagatesRequestID(t *testing.T) {
	capturing := &capturingRequestHandler{}
	handler := NewHandler(capturing)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("X-Request-Id", "req-123")
	req.Header.Set("User-Agent", "Codex/1.2")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if capturing.got.Request == nil {
		t.Fatal("request was not forwarded")
	}
	if got := capturing.got.RequestID; got != "req-123" {
		t.Fatalf("request_id = %q, want %q", got, "req-123")
	}
	if got := capturing.got.Provenance.ClientHandler; got != "codex" {
		t.Fatalf("client handler = %q, want %q", got, "codex")
	}
	if got := capturing.got.Provenance.ClientProtocol; got != "openai_compat" {
		t.Fatalf("client protocol = %q, want %q", got, "openai_compat")
	}
	if got := capturing.got.Provenance.IngressFamily; got != canonical.IngressFamilyChatCompletions {
		t.Fatalf("ingress family = %q, want %q", got, canonical.IngressFamilyChatCompletions)
	}
	if got := capturing.got.Provenance.NormalizedOp; got != canonical.NormalizedPathChatCompletions {
		t.Fatalf("normalized op = %q, want %q", got, canonical.NormalizedPathChatCompletions)
	}
}

func TestHandler_LogsClientProvenanceOnSuccessAndError(t *testing.T) {
	setDefaultLogger, logs := testDebugLogger()
	defer setDefaultLogger()

	success := NewHandler(staticRequestHandler{
		out: requestpath.HandleOutput{
			Response: ports.NewBufferedProviderResponse(
				canonical.NewConversationOutput(
					"chatcmpl_1",
					"m",
					[]canonical.OutputItem{canonical.NewTextOutputItem("text_0", "ok")},
					"stop",
				),
			),
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("User-Agent", "Codex/1.0")
	req.Header.Set("X-Request-Id", "req_success")
	rec := httptest.NewRecorder()
	success.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("success status = %d, want %d", rec.Code, http.StatusOK)
	}

	fail := NewHandler(staticRequestHandler{
		err: canonical.NewBackendError("openai", http.StatusBadGateway, `{"error":"upstream failed"}`, ""),
	})
	reqFail := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hello"}]}`))
	reqFail.Header.Set("User-Agent", "Claude-Code/2.0")
	reqFail.Header.Set("X-Request-Id", "req_fail")
	recFail := httptest.NewRecorder()
	fail.ServeHTTP(recFail, reqFail)
	if recFail.Code != http.StatusBadGateway {
		t.Fatalf("failure status = %d, want %d", recFail.Code, http.StatusBadGateway)
	}

	out := logs.String()
	for _, want := range []string{
		"event=ingress_request_shape",
		"event=request_outcome",
		"request_id=req_success",
		"client_handler=codex",
		"client_protocol=openai_compat",
		"request_id=req_fail",
		"client_handler=claude_code",
		"result=backend_error",
		"error_origin=backend",
		"backend_ref=openai",
		"status_code=502",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("logs missing %q\nlogs:\n%s", want, out)
		}
	}
}

func TestHandler_ServesEndpointModels(t *testing.T) {
	handler := NewHandler(&modelsCapableHandler{
		modelsOut: requestpath.ListModelsOutput{
			DefaultModelID: "openai_compatible:gpt-4o",
			Models: []requestpath.ModelOption{
				{ID: "openai_compatible:gpt-4o", ModelID: "gpt-4o", ProviderSpec: "openai_compatible", BackendRef: "backend-a"},
				{ID: "openai_compatible:gpt-4.1", ModelID: "gpt-4.1", ProviderSpec: "openai_compatible", BackendRef: "backend-b"},
			},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/c/alpha/v1/models", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"object":"list"`) {
		t.Fatalf("body = %q, want list object", body)
	}
	if !strings.Contains(body, `"id":"openai_compatible:gpt-4o"`) {
		t.Fatalf("body = %q, want model id", body)
	}
	if strings.Contains(body, `"swobu_model"`) || strings.Contains(body, `"swobu_default"`) || strings.Contains(body, `"swobu_backend"`) || strings.Contains(body, `"swobu_provider"`) {
		t.Fatalf("body = %q, want OpenAI-shaped model entries without swobu_* fields", body)
	}
}

func TestHandler_ServesEndpointModelsAliasPath(t *testing.T) {
	handler := NewHandler(&modelsCapableHandler{
		modelsOut: requestpath.ListModelsOutput{
			DefaultModelID: "openai_compatible:gpt-4o",
			Models:         []requestpath.ModelOption{{ID: "openai_compatible:gpt-4o", ModelID: "gpt-4o", ProviderSpec: "openai_compatible", BackendRef: "backend-a"}},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/c/alpha/models", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"id":"openai_compatible:gpt-4o"`) {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestHandler_RejectsNonGETModelsRequests(t *testing.T) {
	handler := NewHandler(&modelsCapableHandler{})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/v1/models", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), "UNSUPPORTED_OPERATION") {
		t.Fatalf("body = %q, want UNSUPPORTED_OPERATION", rec.Body.String())
	}
}

func TestHandler_DoesNotExposeSwobuModelHeaders(t *testing.T) {
	resp := ports.NewBufferedProviderResponse(
		canonical.NewConversationOutput(
			"chatcmpl_1",
			"resolved-model",
			[]canonical.OutputItem{canonical.NewTextOutputItem("text_0", "ok")},
			"stop",
		),
	).WithMetadata(ports.ProviderResponseMetadata{
		ModelRequested:      "requested-model",
		ModelResolved:       "resolved-model",
		ModelResolutionMode: "default_unknown",
	})
	handler := NewHandler(staticRequestHandler{
		out: requestpath.HandleOutput{
			Response: resp,
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	for _, key := range []string{"X-Swobu-Model-Requested", "X-Swobu-Model-Resolved", "X-Swobu-Model-Resolution"} {
		if got := rec.Header().Get(key); got != "" {
			t.Fatalf("header %s = %q, want empty", key, got)
		}
	}
}

func TestHandler_DecodesCompressedRequestsAndPreservesStructuredAnthropicContent(t *testing.T) {
	capturing := &capturingRequestHandler{}
	handler := NewHandler(capturing)
	var encoded bytes.Buffer
	gz := gzip.NewWriter(&encoded)
	_, _ = gz.Write([]byte(`{"model":"m","messages":[{"role":"assistant","content":[{"type":"text","text":"working"},{"type":"tool_use","id":"toolu_1","name":"calc","input":{"expr":"2+2"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"4"}]}]}`))
	_ = gz.Close()
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/messages", bytes.NewReader(encoded.Bytes()))
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	typed, ok := capturing.got.Request.(canonical.DialogCanonicalRequest)
	if !ok {
		t.Fatalf("request type = %T, want canonical.DialogCanonicalRequest", capturing.got.Request)
	}
	items := typed.Items()
	if len(items) != 3 {
		t.Fatalf("items len = %d, want 3", len(items))
	}
	if got := items[1].Kind; got != canonical.ItemKindToolUse {
		t.Fatalf("item kind = %q, want %q", got, canonical.ItemKindToolUse)
	}
	if got := items[0].Author; got != canonical.ItemAuthorAssistant {
		t.Fatalf("author = %q, want %q", got, canonical.ItemAuthorAssistant)
	}
	if got := items[2].Kind; got != canonical.ItemKindToolResult {
		t.Fatalf("item kind = %q, want %q", got, canonical.ItemKindToolResult)
	}
	if got := capturing.got.Provenance.ClientProtocol; got != "anthropic_compat" {
		t.Fatalf("client protocol = %q, want %q", got, "anthropic_compat")
	}
	if got := capturing.got.Provenance.IngressFamily; got != canonical.IngressFamilyMessages {
		t.Fatalf("ingress family = %q, want %q", got, canonical.IngressFamilyMessages)
	}
	if got := capturing.got.Provenance.NormalizedOp; got != canonical.NormalizedPathMessages {
		t.Fatalf("normalized op = %q, want %q", got, canonical.NormalizedPathMessages)
	}
}

func TestHandler_RejectsOversizedRequestBody(t *testing.T) {
	capturing := &capturingRequestHandler{}
	handler := NewHandler(capturing)
	oversized := bytes.Repeat([]byte("a"), int(maxCompressedRequestBodyBytes)+1)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewReader(oversized))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "BAD_REQUEST") {
		t.Fatalf("body = %q, want BAD_REQUEST", rec.Body.String())
	}
}

func TestHandler_RejectsDecodedBodyOverLimit(t *testing.T) {
	capturing := &capturingRequestHandler{}
	handler := NewHandler(capturing)

	var encoded bytes.Buffer
	gz := gzip.NewWriter(&encoded)
	_, _ = gz.Write(bytes.Repeat([]byte("x"), int(maxDecodedRequestBodyBytes)+1))
	_ = gz.Close()

	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewReader(encoded.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "BAD_REQUEST") {
		t.Fatalf("body = %q, want BAD_REQUEST", rec.Body.String())
	}
}

func TestHandler_PreservesResponsesStateAndStructuredInput(t *testing.T) {
	capturing := &capturingRequestHandler{}
	handler := NewHandler(capturing)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","previous_response_id":"resp_123","prompt_cache_key":"repo-alpha","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},{"type":"function_call","call_id":"call_1","name":"grep","arguments":{"pattern":"TODO"}},{"type":"function_call_output","call_id":"call_1","output":"2 hits"}]}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	typed, ok := capturing.got.Request.(canonical.GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("request type = %T, want canonical.GenerationCanonicalRequest", capturing.got.Request)
	}
	if got := typed.PreviousResponseID(); got != "resp_123" {
		t.Fatalf("previous_response_id = %q, want %q", got, "resp_123")
	}
	if got := typed.PromptCacheKey(); got != "repo-alpha" {
		t.Fatalf("prompt_cache_key = %q, want %q", got, "repo-alpha")
	}
	items := typed.Thread()
	if len(items) != 3 {
		t.Fatalf("conversation len = %d, want 3", len(items))
	}
	if got := items[1].Kind; got != canonical.ItemKindToolUse {
		t.Fatalf("item kind = %q, want %q", got, canonical.ItemKindToolUse)
	}
	if got := items[2].Kind; got != canonical.ItemKindToolResult {
		t.Fatalf("item kind = %q, want %q", got, canonical.ItemKindToolResult)
	}
}

func TestHandler_DecodesResponsesToolChoiceStrictIntoCanonicalToolMode(t *testing.T) {
	capturing := &capturingRequestHandler{}
	handler := NewHandler(capturing)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","tool_choice":"required","input":"continue"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	typed, ok := capturing.got.Request.(canonical.GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("request type = %T, want canonical.GenerationCanonicalRequest", capturing.got.Request)
	}
	if got := typed.ToolMode(); got != canonical.ToolModeRequired {
		t.Fatalf("tool mode = %q, want %q", got, canonical.ToolModeRequired)
	}
}

func TestHandler_RejectsResponsesRequestsWithBothContinuationSelectors(t *testing.T) {
	capturing := &capturingRequestHandler{}
	handler := NewHandler(capturing)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","previous_response_id":"resp_123","conversation":"conv_123","input":"continue"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if body := rec.Body.String(); !strings.Contains(body, "BAD_REQUEST") {
		t.Fatalf("body = %q, want BAD_REQUEST", body)
	}
}

func TestHandler_RejectsResponsesConversationSelector(t *testing.T) {
	capturing := &capturingRequestHandler{}
	handler := NewHandler(capturing)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","conversation":"conv_123","input":"continue"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if body := rec.Body.String(); !strings.Contains(body, "responses conversation is not supported in swobu v0") {
		t.Fatalf("body = %q, want explicit unsupported conversation message", body)
	}
}

func TestHandler_RejectsUnsupportedRequestContentEncoding(t *testing.T) {
	handler := NewHandler(&capturingRequestHandler{})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Encoding", "brotli")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if body := rec.Body.String(); !strings.Contains(body, "BAD_REQUEST") {
		t.Fatalf("body = %q, want BAD_REQUEST", body)
	}
}

func TestHandler_RejectsWebSocketIngressWithGuidance(t *testing.T) {
	handler := NewHandler(&capturingRequestHandler{})
	req := httptest.NewRequest(http.MethodGet, "/c/alpha/chat/completions", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"code":"UNSUPPORTED_ENDPOINT"`) {
		t.Fatalf("body = %q, want UNSUPPORTED_ENDPOINT", body)
	}
	if !strings.Contains(body, "supported only on protocol /responses routes") {
		t.Fatalf("body = %q, want /responses guidance", body)
	}
}

func TestHandler_AcceptsResponsesWebSocketIngress(t *testing.T) {
	handler := NewHandler(staticRequestHandler{
		out: requestpath.HandleOutput{
			Response: testStreamingTextResponse("resp_1", "m", "text_0", "ok", "completed"),
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/c/alpha/responses"
	cfg, err := websocket.NewConfig(wsURL, server.URL)
	if err != nil {
		t.Fatalf("NewConfig returned error: %v", err)
	}
	cfg.Header.Set("User-Agent", "Codex/0.122.0")
	conn, err := websocket.DialConfig(cfg)
	if err != nil {
		t.Fatalf("DialConfig returned error: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	if err := websocket.Message.Send(conn, `{"type":"response.create","model":"m","input":"hi","stream":true}`); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	var frames []string
	for {
		var message string
		if err := websocket.Message.Receive(conn, &message); err != nil {
			t.Fatalf("Receive returned error: %v", err)
		}
		frames = append(frames, message)
		if strings.Contains(message, `"type":"response.completed"`) {
			break
		}
	}

	joined := strings.Join(frames, "\n")
	if !strings.Contains(joined, `"type":"response.created"`) {
		t.Fatalf("frames = %q, want response.created", joined)
	}
	if !strings.Contains(joined, `"type":"response.output_item.added"`) {
		t.Fatalf("frames = %q, want response.output_item.added", joined)
	}
	if !strings.Contains(joined, `"type":"response.content_part.added"`) {
		t.Fatalf("frames = %q, want response.content_part.added", joined)
	}
	if !strings.Contains(joined, `"type":"response.output_text.delta"`) {
		t.Fatalf("frames = %q, want response.output_text.delta", joined)
	}
	if !strings.Contains(joined, `"item_id":"text_0"`) {
		t.Fatalf("frames = %q, want item_id linkage", joined)
	}
	if !strings.Contains(joined, `"type":"response.output_item.done"`) {
		t.Fatalf("frames = %q, want response.output_item.done", joined)
	}
	if !strings.Contains(joined, `"type":"response.completed"`) {
		t.Fatalf("frames = %q, want response.completed", joined)
	}
	if strings.Contains(joined, `"type":"error"`) {
		t.Fatalf("frames = %q, want no error events", joined)
	}
	if strings.Index(joined, `"type":"response.output_item.added"`) > strings.Index(joined, `"type":"response.output_text.delta"`) {
		t.Fatalf("frames = %q, want output_item.added before output_text.delta", joined)
	}
}

func TestHandler_ResponsesWebSocketRejectsUnsupportedMessageType(t *testing.T) {
	handler := NewHandler(staticRequestHandler{
		out: requestpath.HandleOutput{
			Response: testStreamingEmptyResponse(),
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/c/alpha/responses"
	cfg, err := websocket.NewConfig(wsURL, server.URL)
	if err != nil {
		t.Fatalf("NewConfig returned error: %v", err)
	}
	conn, err := websocket.DialConfig(cfg)
	if err != nil {
		t.Fatalf("DialConfig returned error: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	if err := websocket.Message.Send(conn, `{"type":"response.cancel"}`); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	var message string
	if err := websocket.Message.Receive(conn, &message); err != nil {
		t.Fatalf("Receive returned error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(message), &got); err != nil {
		t.Fatalf("error frame json decode failed: %v", err)
	}
	if got["type"] != "error" {
		t.Fatalf("frame = %s, want type=error", message)
	}
}

func TestHandler_RejectsNonPOSTCompatibilityFamilyOperations(t *testing.T) {
	handler := NewHandler(&capturingRequestHandler{})
	req := httptest.NewRequest(http.MethodGet, "/c/alpha/responses", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"code":"UNSUPPORTED_ENDPOINT"`) {
		t.Fatalf("body = %q, want UNSUPPORTED_ENDPOINT", body)
	}
	if !strings.Contains(body, "protocol family operations require HTTP POST") {
		t.Fatalf("body = %q, want POST guidance", body)
	}
}

func TestHandler_RejectsUnsupportedAnthropicMessagePartType(t *testing.T) {
	handler := NewHandler(&capturingRequestHandler{})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/messages", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}]}]}`))
	req.Header.Set("anthropic-version", "2023-06-01")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if body := rec.Body.String(); !strings.Contains(body, "unsupported part type") {
		t.Fatalf("body = %q, want unsupported part type failure", body)
	}
}

func TestHandler_EncodesToolCallStreamingForResponses(t *testing.T) {
	handler := NewHandler(staticRequestHandler{
		out: requestpath.HandleOutput{
			Response: testStreamingToolResponse("resp_1", "m", "tool_0", "call_1", "grep", []string{`{"pattern":"TO`, `DO"}`}, "completed"),
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","input":"hi","stream":true}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"response.function_call_arguments.delta"`) {
		t.Fatalf("body = %q, want function_call_arguments.delta", body)
	}
	if !strings.Contains(body, `"call_id":"call_1"`) {
		t.Fatalf("body = %q, want call_id", body)
	}
	if !strings.Contains(body, `"type":"response.function_call_arguments.done"`) {
		t.Fatalf("body = %q, want function_call_arguments.done", body)
	}
}

func TestHandler_EncodesTextStreamingLifecycleForResponses(t *testing.T) {
	handler := NewHandler(staticRequestHandler{
		out: requestpath.HandleOutput{
			Response: testStreamingTextResponse("resp_1", "m", "text_0", "ok", "completed"),
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","input":"hi","stream":true}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"response.output_item.added"`) {
		t.Fatalf("body = %q, want output_item.added", body)
	}
	if !strings.Contains(body, `"type":"response.content_part.added"`) {
		t.Fatalf("body = %q, want content_part.added", body)
	}
	if !strings.Contains(body, `"type":"response.output_text.delta"`) {
		t.Fatalf("body = %q, want output_text.delta", body)
	}
	if !strings.Contains(body, `"type":"response.output_text.done"`) {
		t.Fatalf("body = %q, want output_text.done", body)
	}
	if !strings.Contains(body, `"type":"response.output_item.done"`) {
		t.Fatalf("body = %q, want output_item.done", body)
	}
	if strings.Index(body, `"type":"response.output_item.added"`) > strings.Index(body, `"type":"response.output_text.delta"`) {
		t.Fatalf("body = %q, want output_item.added before output_text.delta", body)
	}
}

func TestHandler_EncodesToolCallStreamingForMessages(t *testing.T) {
	handler := NewHandler(staticRequestHandler{
		out: requestpath.HandleOutput{
			Response: testStreamingToolResponse("msg_1", "m", "tool_0", "call_1", "grep", []string{`{"pattern":"TODO"}`}, "tool_use"),
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/messages", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	req.Header.Set("anthropic-version", "2023-06-01")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"tool_use"`) {
		t.Fatalf("body = %q, want tool_use block", body)
	}
	if !strings.Contains(body, `"type":"input_json_delta"`) {
		t.Fatalf("body = %q, want input_json_delta", body)
	}
	if !strings.Contains(body, `"stop_reason":"tool_use"`) {
		t.Fatalf("body = %q, want tool_use stop_reason", body)
	}
}

type capturingRequestHandler struct {
	got requestpath.HandleInput
}

func (h *capturingRequestHandler) Handle(_ context.Context, in requestpath.HandleInput) (requestpath.HandleOutput, error) {
	h.got = in
	return requestpath.HandleOutput{
		Response: ports.NewBufferedProviderResponse(
			canonical.NewConversationOutput(
				"chatcmpl_1",
				"m",
				[]canonical.OutputItem{
					canonical.NewTextOutputItem("text_0", "ok"),
				},
				"stop",
			),
		),
		Target: ports.NewRoutableTarget("backend-a", "openai_compatible", "https://example.test/v1", "cred-1", "chat_completions", ""),
	}, nil
}

type staticRequestHandler struct {
	out requestpath.HandleOutput
	err error
}

func (h staticRequestHandler) Handle(_ context.Context, _ requestpath.HandleInput) (requestpath.HandleOutput, error) {
	return h.out, h.err
}

func testStreamingEmptyResponse() ports.ProviderResponse {
	return ports.NewEnvelopeStreamingProviderResponse(canonical.NewSliceEventReader(nil))
}

func testDebugLogger() (restore func(), out *bytes.Buffer) {
	var buf bytes.Buffer
	prev := slog.Default()
	next := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(next)
	return func() {
		slog.SetDefault(prev)
	}, &buf
}

func testStreamingTextResponse(resultID string, model string, itemID string, text string, finish string) ports.ProviderResponse {
	events := []canonical.Event{
		{ExchangeID: "test_exchange", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{ExchangeID: "test_exchange", Seq: 2, Kind: canonical.EventMetadata, EnvID: "res_1", Payload: canonical.MetadataPayload{Values: map[string]string{"result_id": resultID, "model": model}}},
		{ExchangeID: "test_exchange", Seq: 3, Kind: canonical.EventEnvelopeStart, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant}, Meta: canonical.EventMetadataFields{NativeID: itemID}},
		{ExchangeID: "test_exchange", Seq: 4, Kind: canonical.EventTextDelta, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.TextDeltaPayload{Text: text}},
		{ExchangeID: "test_exchange", Seq: 5, Kind: canonical.EventEnvelopeEnd, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvMessage, Status: canonical.EnvelopeStatusCompleted}},
		{ExchangeID: "test_exchange", Seq: 6, Kind: canonical.EventFinish, EnvID: "res_1", Payload: canonical.FinishPayload{Reason: finish}},
		{ExchangeID: "test_exchange", Seq: 7, Kind: canonical.EventEnvelopeEnd, EnvID: "res_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	}
	return ports.NewEnvelopeStreamingProviderResponse(canonical.NewSliceEventReader(events))
}

func testStreamingToolResponse(resultID string, model string, itemID string, toolUseID string, name string, argDeltas []string, finish string) ports.ProviderResponse {
	events := []canonical.Event{
		{ExchangeID: "test_exchange", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{ExchangeID: "test_exchange", Seq: 2, Kind: canonical.EventMetadata, EnvID: "res_1", Payload: canonical.MetadataPayload{Values: map[string]string{"result_id": resultID, "model": model}}},
		{ExchangeID: "test_exchange", Seq: 3, Kind: canonical.EventEnvelopeStart, EnvID: "tool_1", ParentID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvToolCall, Name: name, ToolUseID: toolUseID}, Meta: canonical.EventMetadataFields{NativeID: itemID}},
	}
	seq := int64(4)
	for _, delta := range argDeltas {
		events = append(events, canonical.Event{ExchangeID: "test_exchange", Seq: seq, Kind: canonical.EventArgsDelta, EnvID: "tool_1", ParentID: "res_1", Payload: canonical.ArgsDeltaPayload{Args: delta}})
		seq++
	}
	events = append(events,
		canonical.Event{ExchangeID: "test_exchange", Seq: seq, Kind: canonical.EventEnvelopeEnd, EnvID: "tool_1", ParentID: "res_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvToolCall, Status: canonical.EnvelopeStatusCompleted}},
		canonical.Event{ExchangeID: "test_exchange", Seq: seq + 1, Kind: canonical.EventFinish, EnvID: "res_1", Payload: canonical.FinishPayload{Reason: finish}},
		canonical.Event{ExchangeID: "test_exchange", Seq: seq + 2, Kind: canonical.EventEnvelopeEnd, EnvID: "res_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	)
	return ports.NewEnvelopeStreamingProviderResponse(canonical.NewSliceEventReader(events))
}

type modelsCapableHandler struct {
	modelsOut   requestpath.ListModelsOutput
	modelsErr   error
	gotModelsIn requestpath.ListModelsInput
}

func (h *modelsCapableHandler) Handle(_ context.Context, _ requestpath.HandleInput) (requestpath.HandleOutput, error) {
	return requestpath.HandleOutput{
		Response: ports.NewBufferedProviderResponse(
			canonical.NewConversationOutput(
				"chatcmpl_1",
				"m",
				[]canonical.OutputItem{
					canonical.NewTextOutputItem("text_0", "ok"),
				},
				"stop",
			),
		),
	}, nil
}

func (h *modelsCapableHandler) ListModels(_ context.Context, in requestpath.ListModelsInput) (requestpath.ListModelsOutput, error) {
	h.gotModelsIn = in
	return h.modelsOut, h.modelsErr
}
