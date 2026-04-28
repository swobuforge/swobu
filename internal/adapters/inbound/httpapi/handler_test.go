package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/websocket"

	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/ports"
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
	if got := capturing.got.Provenance.IngressFamily; got != compatibility.IngressFamilyChatCompletions {
		t.Fatalf("ingress family = %q, want %q", got, compatibility.IngressFamilyChatCompletions)
	}
	if got := capturing.got.Provenance.NormalizedOp; got != compatibility.NormalizedPathChatCompletions {
		t.Fatalf("normalized op = %q, want %q", got, compatibility.NormalizedPathChatCompletions)
	}
}

func TestHandler_ServesEndpointModels(t *testing.T) {
	handler := NewHandler(&modelsCapableHandler{
		modelsOut: requestpath.ListModelsOutput{
			DefaultModelID: "custom:gpt-4o",
			Models: []requestpath.ModelOption{
				{ID: "custom:gpt-4o", ModelID: "gpt-4o", ProviderSpec: "custom", BackendRef: "backend-a"},
				{ID: "custom:gpt-4.1", ModelID: "gpt-4.1", ProviderSpec: "custom", BackendRef: "backend-b"},
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
	if !strings.Contains(body, `"id":"custom:gpt-4o"`) {
		t.Fatalf("body = %q, want model id", body)
	}
	if strings.Contains(body, `"swobu_model"`) || strings.Contains(body, `"swobu_default"`) || strings.Contains(body, `"swobu_backend"`) || strings.Contains(body, `"swobu_provider"`) {
		t.Fatalf("body = %q, want OpenAI-shaped model entries without swobu_* fields", body)
	}
}

func TestHandler_ServesEndpointModelsAliasPath(t *testing.T) {
	handler := NewHandler(&modelsCapableHandler{
		modelsOut: requestpath.ListModelsOutput{
			DefaultModelID: "custom:gpt-4o",
			Models:         []requestpath.ModelOption{{ID: "custom:gpt-4o", ModelID: "gpt-4o", ProviderSpec: "custom", BackendRef: "backend-a"}},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/c/alpha/models", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"id":"custom:gpt-4o"`) {
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

func TestHandler_WritesModelResolutionHeaders(t *testing.T) {
	resp := ports.NewBufferedExecuteResponse(
		compatibility.NewConversationOutput(
			"chatcmpl_1",
			"resolved-model",
			[]compatibility.OutputItem{compatibility.NewTextOutputItem("text_0", "ok")},
			"stop",
		),
	).WithMetadata(ports.ExecuteMetadata{
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
	if got := rec.Header().Get("X-Swobu-Model-Requested"); got != "requested-model" {
		t.Fatalf("requested model header = %q", got)
	}
	if got := rec.Header().Get("X-Swobu-Model-Resolved"); got != "resolved-model" {
		t.Fatalf("resolved model header = %q", got)
	}
	if got := rec.Header().Get("X-Swobu-Model-Resolution"); got != "default_unknown" {
		t.Fatalf("resolution header = %q", got)
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
	typed, ok := capturing.got.Request.(compatibility.DialogCanonicalRequest)
	if !ok {
		t.Fatalf("request type = %T, want compatibility.DialogCanonicalRequest", capturing.got.Request)
	}
	items := typed.Items()
	if len(items) != 3 {
		t.Fatalf("items len = %d, want 3", len(items))
	}
	if got := items[1].Kind; got != compatibility.ItemKindToolUse {
		t.Fatalf("item kind = %q, want %q", got, compatibility.ItemKindToolUse)
	}
	if got := items[0].Author; got != compatibility.ItemAuthorAssistant {
		t.Fatalf("author = %q, want %q", got, compatibility.ItemAuthorAssistant)
	}
	if got := items[2].Kind; got != compatibility.ItemKindToolResult {
		t.Fatalf("item kind = %q, want %q", got, compatibility.ItemKindToolResult)
	}
	if got := capturing.got.Provenance.ClientProtocol; got != "anthropic_compat" {
		t.Fatalf("client protocol = %q, want %q", got, "anthropic_compat")
	}
	if got := capturing.got.Provenance.IngressFamily; got != compatibility.IngressFamilyMessages {
		t.Fatalf("ingress family = %q, want %q", got, compatibility.IngressFamilyMessages)
	}
	if got := capturing.got.Provenance.NormalizedOp; got != compatibility.NormalizedPathMessages {
		t.Fatalf("normalized op = %q, want %q", got, compatibility.NormalizedPathMessages)
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
	typed, ok := capturing.got.Request.(compatibility.GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("request type = %T, want compatibility.GenerationCanonicalRequest", capturing.got.Request)
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
	if got := items[1].Kind; got != compatibility.ItemKindToolUse {
		t.Fatalf("item kind = %q, want %q", got, compatibility.ItemKindToolUse)
	}
	if got := items[2].Kind; got != compatibility.ItemKindToolResult {
		t.Fatalf("item kind = %q, want %q", got, compatibility.ItemKindToolResult)
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
	typed, ok := capturing.got.Request.(compatibility.GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("request type = %T, want compatibility.GenerationCanonicalRequest", capturing.got.Request)
	}
	if got := typed.ToolMode(); got != compatibility.ToolModeRequired {
		t.Fatalf("tool mode = %q, want %q", got, compatibility.ToolModeRequired)
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
	if !strings.Contains(body, "supported only on compatibility /responses routes") {
		t.Fatalf("body = %q, want /responses guidance", body)
	}
}

func TestHandler_AcceptsResponsesWebSocketIngress(t *testing.T) {
	handler := NewHandler(staticRequestHandler{
		out: requestpath.HandleOutput{
			Response: ports.NewStreamingExecuteResponse(compatibility.NewSliceEventStream([]compatibility.OutputEvent{
				{Kind: compatibility.OutputEventStarted, ResultID: "resp_1", Model: "m"},
				{Kind: compatibility.OutputEventTextDelta, ItemID: "text_0", TextDelta: "ok"},
				{Kind: compatibility.OutputEventCompleted, FinishReason: "completed"},
			})),
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
			Response: ports.NewStreamingExecuteResponse(compatibility.NewSliceEventStream(nil)),
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
	if !strings.Contains(body, "compatibility family operations require HTTP POST") {
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
			Response: ports.NewStreamingExecuteResponse(compatibility.NewSliceEventStream([]compatibility.OutputEvent{
				{Kind: compatibility.OutputEventStarted, ResultID: "resp_1", Model: "m"},
				{Kind: compatibility.OutputEventItemStarted, ItemKind: compatibility.OutputItemToolUse, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep"},
				{Kind: compatibility.OutputEventToolUseArgumentsDelta, ItemKind: compatibility.OutputItemToolUse, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep", ArgumentsDelta: "{\"pattern\":\"TO"},
				{Kind: compatibility.OutputEventToolUseArgumentsDelta, ItemKind: compatibility.OutputItemToolUse, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep", ArgumentsDelta: "DO\"}"},
				{Kind: compatibility.OutputEventItemCompleted, ItemKind: compatibility.OutputItemToolUse, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep"},
				{Kind: compatibility.OutputEventCompleted, FinishReason: "completed"},
			})),
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
			Response: ports.NewStreamingExecuteResponse(compatibility.NewSliceEventStream([]compatibility.OutputEvent{
				{Kind: compatibility.OutputEventStarted, ResultID: "resp_1", Model: "m"},
				{Kind: compatibility.OutputEventTextDelta, ItemID: "text_0", TextDelta: "ok"},
				{Kind: compatibility.OutputEventCompleted, FinishReason: "completed"},
			})),
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
			Response: ports.NewStreamingExecuteResponse(compatibility.NewSliceEventStream([]compatibility.OutputEvent{
				{Kind: compatibility.OutputEventStarted, ResultID: "msg_1", Model: "m"},
				{Kind: compatibility.OutputEventItemStarted, ItemKind: compatibility.OutputItemToolUse, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep"},
				{Kind: compatibility.OutputEventToolUseArgumentsDelta, ItemKind: compatibility.OutputItemToolUse, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep", ArgumentsDelta: "{\"pattern\":\"TODO\"}"},
				{Kind: compatibility.OutputEventItemCompleted, ItemKind: compatibility.OutputItemToolUse, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep"},
				{Kind: compatibility.OutputEventCompleted, FinishReason: "tool_use"},
			})),
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
		Response: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"chatcmpl_1",
				"m",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				},
				"stop",
			),
		),
		Target: ports.NewRoutableTarget("backend-a", "custom", "https://example.test/v1", "cred-1", "chat_completions", "", ""),
	}, nil
}

type staticRequestHandler struct {
	out requestpath.HandleOutput
	err error
}

func (h staticRequestHandler) Handle(_ context.Context, _ requestpath.HandleInput) (requestpath.HandleOutput, error) {
	return h.out, h.err
}

type modelsCapableHandler struct {
	modelsOut   requestpath.ListModelsOutput
	modelsErr   error
	gotModelsIn requestpath.ListModelsInput
}

func (h *modelsCapableHandler) Handle(_ context.Context, _ requestpath.HandleInput) (requestpath.HandleOutput, error) {
	return requestpath.HandleOutput{
		Response: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"chatcmpl_1",
				"m",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
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
