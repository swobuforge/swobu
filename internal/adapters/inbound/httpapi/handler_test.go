package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/websocket"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
	chatcompletions "github.com/swobuforge/swobu/internal/wire/chatcompletions"
	completions "github.com/swobuforge/swobu/internal/wire/completions"
	messages "github.com/swobuforge/swobu/internal/wire/messages"
	responses "github.com/swobuforge/swobu/internal/wire/responses"
)

func TestHandler_ForwardsCanonicalRequest(t *testing.T) {
	capturing := &capturingRequestIngress{}
	handler := NewHandler(capturing)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("X-Request-Id", "req-123")
	req.Header.Set("User-Agent", "Codex/1.2")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	request := testDecodeCapturedRequest(t, capturing.got)
	if request.Model() == "" {
		t.Fatal("request was not forwarded")
	}
}

func TestHandler_LogsClientProvenanceOnSuccessAndError(t *testing.T) {
	setDefaultLogger, logs := testDebugLogger()
	defer setDefaultLogger()

	success := NewHandler(staticRequestIngress{
		envelope: testProviderIngressFromOutput(
			canonical.NewConversationOutput(
				"chatcmpl_1",
				"m",
				[]canonical.OutputItem{canonical.NewTextOutputItem("text_0", "ok")},
				"stop",
			),
		),
	})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("User-Agent", "Codex/1.0")
	req.Header.Set("X-Request-Id", "req_success")
	rec := httptest.NewRecorder()
	success.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("success status = %d, want %d", rec.Code, http.StatusOK)
	}

	fail := NewHandler(staticRequestIngress{
		err: canonical.NewBackendError("openai", http.StatusBadGateway, `{"error":"provider failed"}`, ""),
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
		"request_id=req_fail",
		"result=backend_error",
		"error_origin=backend",
		"backend_ref=openai",
		"error_message=\"backend error from openai (502): {\\\"error\\\":\\\"provider failed\\\"}\"",
		"status_code=502",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("logs missing %q\nlogs:\n%s", want, out)
		}
	}
}

func TestHandler_LogsSwobuErrorDetailsOnFailure(t *testing.T) {
	setDefaultLogger, logs := testDebugLogger()
	defer setDefaultLogger()

	handler := NewHandler(staticRequestIngress{
		err: canonical.UnsupportedOperation("chat completions protocol only supports function and custom tool declarations; got namespace"),
	})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("User-Agent", "Codex/1.0")
	req.Header.Set("X-Request-Id", "req_swobu")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("failure status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	out := logs.String()
	for _, want := range []string{
		"event=request_outcome",
		"request_id=req_swobu",
		"result=swobu_error",
		"error_origin=swobu",
		"error_code=UNSUPPORTED_OPERATION",
		"error_message=\"UNSUPPORTED_OPERATION: chat completions protocol only supports function and custom tool declarations; got namespace\"",
		"status_code=400",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("logs missing %q\nlogs:\n%s", want, out)
		}
	}
}

func TestHandler_LogsResponsesToolReferenceDetailsOnFailure(t *testing.T) {
	setDefaultLogger, logs := testDebugLogger()
	defer setDefaultLogger()

	handler := NewHandler(staticRequestIngress{
		err: canonical.Error{
			Code:    canonical.ErrorCodeBadRequest,
			Message: `responses request tools[].name (function) name "exec_command__bogus" is invalid: canonical request tool references are undeclared tool`,
			Origin:  canonical.ErrorOriginSwobu,
			Details: map[string]string{
				"request_field": "tools[].name",
				"tool_kind":     "function",
				"tool_name":     "exec_command__bogus",
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","input":"ping","tools":[{"type":"function","name":"exec_command__bogus","parameters":{"type":"object","properties":{"pattern":{"type":"string"}}}}]}`))
	req.Header.Set("User-Agent", "Codex/1.0")
	req.Header.Set("X-Request-Id", "req_tool_ref")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("failure status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	out := logs.String()
	for _, want := range []string{
		"event=request_outcome",
		"request_id=req_tool_ref",
		"result=swobu_error",
		"error_origin=swobu",
		"error_code=BAD_REQUEST",
		`error_message="BAD_REQUEST: responses request tools[].name (function) name \"exec_command__bogus\" is invalid: canonical request tool references are undeclared tool"`,
		"error_detail_request_field=tools[].name",
		"error_detail_tool_kind=function",
		"error_detail_tool_name=exec_command__bogus",
		"status_code=400",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("logs missing %q\nlogs:\n%s", want, out)
		}
	}
}

func TestHandler_ServesEndpointModels(t *testing.T) {
	handler := NewHandler(&modelsCapableHandler{
		modelsOut: exchange.ListModelsOutput{
			DefaultModelID: "openai_compatible:gpt-4o",
			Models: []exchange.ModelOption{
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
	if !strings.Contains(body, `"name":"openai_compatible:gpt-4o"`) {
		t.Fatalf("body = %q, want model name", body)
	}
	if strings.Contains(body, `"swobu_model"`) || strings.Contains(body, `"swobu_default"`) || strings.Contains(body, `"swobu_backend"`) || strings.Contains(body, `"swobu_provider"`) {
		t.Fatalf("body = %q, want OpenAI-shaped model entries without swobu_* fields", body)
	}
}

func TestHandler_ServesEndpointModelsAliasPath(t *testing.T) {
	handler := NewHandler(&modelsCapableHandler{
		modelsOut: exchange.ListModelsOutput{
			DefaultModelID: "openai_compatible:gpt-4o",
			Models:         []exchange.ModelOption{{ID: "openai_compatible:gpt-4o", ModelID: "gpt-4o", ProviderSpec: "openai_compatible", BackendRef: "backend-a"}},
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
	if !strings.Contains(rec.Body.String(), `"name":"openai_compatible:gpt-4o"`) {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestHandler_RejectsNonGETModelsRequests(t *testing.T) {
	handler := NewHandler(&modelsCapableHandler{})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/v1/models", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "UNSUPPORTED_OPERATION") {
		t.Fatalf("body = %q, want UNSUPPORTED_OPERATION", rec.Body.String())
	}
}

func TestHandler_DoesNotExposeSwobuModelHeaders(t *testing.T) {
	resp := testProviderIngressFromOutput(
		canonical.NewConversationOutput(
			"chatcmpl_1",
			"resolved-model",
			[]canonical.OutputItem{canonical.NewTextOutputItem("text_0", "ok")},
			"stop",
		),
	)
	handler := NewHandler(staticRequestIngress{
		envelope: resp,
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
	capturing := &capturingRequestIngress{}
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
	typed := testDecodeCapturedRequest(t, capturing.got)
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
}

func TestHandler_RejectsOversizedRequestBody(t *testing.T) {
	capturing := &capturingRequestIngress{}
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
	capturing := &capturingRequestIngress{}
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

func TestHandler_AcceptsUnexpectedTopLevelFieldInRequestBody(t *testing.T) {
	capturing := &capturingRequestIngress{}
	handler := NewHandler(capturing)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}],"unexpected":true}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	typed := testDecodeCapturedRequest(t, capturing.got)
	if typed.Model() != "m" {
		t.Fatalf("model = %q, want %q", typed.Model(), "m")
	}
	items := typed.Items()
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].Text != "hi" {
		t.Fatalf("item text = %q, want %q", items[0].Text, "hi")
	}
}

func TestHandler_PreservesResponsesStateAndStructuredInput(t *testing.T) {
	capturing := &capturingRequestIngress{}
	handler := NewHandler(capturing)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","previous_response_id":"resp_123","prompt_cache_key":"repo-alpha","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},{"type":"function_call","call_id":"call_1","name":"grep","arguments":{"pattern":"TODO"}},{"type":"function_call_output","call_id":"call_1","output":"2 hits"}]}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	typed := testDecodeCapturedRequest(t, capturing.got)
	if got, ok := typed.Turn().PreviousID(); !ok || got.String() != "resp_123" {
		t.Fatalf("previous_response_id = %q, want %q", got, "resp_123")
	}
	items := typed.Items()
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

func TestHandler_DecodesResponsesToolChoiceStrictIntoCanonicalToolPolicy(t *testing.T) {
	capturing := &capturingRequestIngress{}
	handler := NewHandler(capturing)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","tool_choice":"required","input":"continue"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	typed := testDecodeCapturedRequest(t, capturing.got)
	if got := typed.ToolPolicy(); got.Mode != canonical.ToolPolicyRequired {
		t.Fatalf("tool policy mode = %q, want %q", got.Mode, canonical.ToolPolicyRequired)
	}
}

func TestHandler_DecodesResponsesSpecificToolChoiceIntoCanonicalToolPolicy(t *testing.T) {
	capturing := &capturingRequestIngress{}
	handler := NewHandler(capturing)
	functionTool := canonical.NewFunctionToolDecl("grep", "grep", "search text", canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`))
	projectedFunctionName, err := canonical.ProjectedToolName(functionTool)
	if err != nil {
		t.Fatalf("ProjectedToolName(function) returned error: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","tools":[{"type":"function","name":"`+projectedFunctionName+`","description":"search text","parameters":{"type":"object","properties":{"pattern":{"type":"string"}}}}],"tool_choice":{"type":"function","name":"`+projectedFunctionName+`"},"input":"continue"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	typed := testDecodeCapturedRequest(t, capturing.got)
	if got := typed.ToolPolicy(); got.Mode != canonical.ToolPolicySpecific {
		t.Fatalf("tool policy mode = %q, want %q", got.Mode, canonical.ToolPolicySpecific)
	}
	wantSpecific := canonical.NewSemanticToolIDFor(canonical.ToolOriginRequest, canonical.ToolKindFunction, "grep").String()
	if specific, ok := typed.ToolPolicy().SpecificID(); !ok || specific.String() != wantSpecific {
		t.Fatalf("tool policy specific = %q, want %q", specific, wantSpecific)
	}
}

func TestHandler_RejectsResponsesRequestsWithBothContinuationSelectors(t *testing.T) {
	capturing := &capturingRequestIngress{}
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
	capturing := &capturingRequestIngress{}
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
	handler := NewHandler(&capturingRequestIngress{})
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

func TestHandler_RejectsWebSocketClientWithGuidance(t *testing.T) {
	handler := NewHandler(&capturingRequestIngress{})
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

func TestHandler_AcceptsResponsesWebSocketClient(t *testing.T) {
	handler := NewHandler(staticRequestIngress{
		envelope: testStreamingTextResponse("resp_1", "m", "text_0", "ok", "completed"),
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
	handler := NewHandler(staticRequestIngress{
		envelope: testStreamingEmptyResponse(),
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
	handler := NewHandler(&capturingRequestIngress{})
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
	if !strings.Contains(body, "protocol family operations require request-post method") {
		t.Fatalf("body = %q, want POST guidance", body)
	}
}

func TestHandler_RejectsUnsupportedAnthropicMessagePartType(t *testing.T) {
	handler := NewHandler(&capturingRequestIngress{})
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
	handler := NewHandler(staticRequestIngress{
		envelope: testStreamingToolResponse("resp_1", "m", "tool_0", "call_1", "grep", []string{`{"pattern":"TO`, `DO"}`}, "completed"),
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
	handler := NewHandler(staticRequestIngress{
		envelope: testStreamingTextResponse("resp_1", "m", "text_0", "ok", "completed"),
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

func TestFallbackAfterStreamCommit(t *testing.T) {
	assertNoExchangeErrorAfterStreamingCommit(t)
}

func TestHandler_DoesNotWriteExchangeErrorAfterStreamingCommit(t *testing.T) {
	assertNoExchangeErrorAfterStreamingCommit(t)
}

func TestHandler_DoesNotLogAfterCommitOnStreamingDisconnect(t *testing.T) {
	setDefaultLogger, logs := testDebugLogger()
	defer setDefaultLogger()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := NewHandler(staticRequestIngress{
		out: exchange.RequestOutput{
			Response: exchange.TransportResponse{
				Transport: transportpkg.TransportResponse{
					Status: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"text/event-stream"},
					},
					Body: &firstChunkThenErrorBody{},
				},
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req = req.WithContext(ctx)
	rec := &writeHeaderCountingResponseWriter{
		cancelAfterWriteCount: 1,
		cancel:                cancel,
	}

	handler.ServeHTTP(rec, req)

	if rec.writeHeaderCount != 1 {
		t.Fatalf("writeHeader count = %d, want 1", rec.writeHeaderCount)
	}
	if rec.writeCount == 0 {
		t.Fatal("body was not written")
	}
	if strings.Contains(logs.String(), "response_write_after_commit_failed") {
		t.Fatalf("logs unexpectedly contain after-commit failure:\n%s", logs.String())
	}
}

func assertNoExchangeErrorAfterStreamingCommit(t *testing.T) {
	t.Helper()

	handler := NewHandler(staticRequestIngress{
		out: exchange.RequestOutput{
			Response: exchange.TransportResponse{
				Transport: transportpkg.TransportResponse{
					Status: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"text/event-stream"},
					},
					Body: &firstChunkThenErrorBody{},
				},
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	rec := &writeHeaderCountingResponseWriter{}

	handler.ServeHTTP(rec, req)

	if rec.writeHeaderCount != 1 {
		t.Fatalf("writeHeader count = %d, want 1", rec.writeHeaderCount)
	}
	if rec.writeCount == 0 {
		t.Fatal("body was not written")
	}
}

func TestHandler_EncodesToolCallStreamingForMessages(t *testing.T) {
	handler := NewHandler(staticRequestIngress{
		envelope: testStreamingToolResponse("msg_1", "m", "tool_0", "call_1", "grep", []string{`{"pattern":"TODO"}`}, "tool_use"),
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

type capturingRequestIngress struct {
	got exchange.RequestInput
}

func (h *capturingRequestIngress) HandleRequest(_ context.Context, in exchange.RequestInput) (exchange.RequestOutput, error) {
	clones, err := replicateRequestInputForTest(in, 3)
	if err != nil {
		return exchange.RequestOutput{}, err
	}
	h.got = clones[0]
	if _, _, err := decodeCapturedRequest(clones[1]); err != nil {
		return exchange.RequestOutput{}, err
	}
	out, err := synthesizeRequestOutputFromEnvelope(clones[2], testProviderIngressFromOutput(
		canonical.NewConversationOutput(
			"chatcmpl_1",
			"m",
			[]canonical.OutputItem{
				canonical.NewTextOutputItem("text_0", "ok"),
			},
			"stop",
		),
	))
	if err != nil {
		return exchange.RequestOutput{}, err
	}
	out.Target = exchange.NewRoutableTarget("backend-a", "openai_compatible", "https://example.test/v1", "cred-1", "chat_completions", "", "", "")
	return out, nil
}

type staticRequestIngress struct {
	out      exchange.RequestOutput
	err      error
	envelope canonical.EventReader
}

func (h staticRequestIngress) HandleRequest(_ context.Context, in exchange.RequestInput) (exchange.RequestOutput, error) {
	if h.envelope != nil {
		return synthesizeRequestOutputFromEnvelope(in, h.envelope)
	}
	return h.out, h.err
}

func testProviderIngressFromOutput(output canonical.CanonicalOutput) canonical.EventReader {
	return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		"test_buffered:httpapi",
		output.ResultID(),
		output.Model(),
		output.Items(),
		output.FinishReason(),
		output.Usage(),
	))
}

func testStreamingEmptyResponse() canonical.EventReader {
	return canonical.NewSliceEventReader(nil)
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

func testStreamingTextResponse(resultID string, model string, itemID string, text string, finish string) canonical.EventReader {
	events := canonical.EventSequence{
		{ExchangeID: "test_exchange", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{ExchangeID: "test_exchange", Seq: 2, Kind: canonical.EventMetadata, EnvID: "res_1", Payload: canonical.MetadataPayload{Values: map[string]string{"result_id": resultID, "model": model}}},
		{ExchangeID: "test_exchange", Seq: 3, Kind: canonical.EventEnvelopeStart, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvMessage, Role: canonical.ItemAuthorAssistant}, Meta: canonical.EventMetadataFields{NativeID: itemID}},
		{ExchangeID: "test_exchange", Seq: 4, Kind: canonical.EventTextDelta, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.TextDeltaPayload{Text: text}},
		{ExchangeID: "test_exchange", Seq: 5, Kind: canonical.EventEnvelopeEnd, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvMessage, Status: canonical.EnvelopeStatusCompleted}},
		{ExchangeID: "test_exchange", Seq: 6, Kind: canonical.EventFinish, EnvID: "res_1", Payload: canonical.FinishPayload{Reason: finish}},
		{ExchangeID: "test_exchange", Seq: 7, Kind: canonical.EventEnvelopeEnd, EnvID: "res_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	}
	return canonical.NewSliceEventReader(events)
}

func testStreamingToolResponse(resultID string, model string, itemID string, toolUseID string, name string, argDeltas []string, finish string) canonical.EventReader {
	events := canonical.EventSequence{
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
	return canonical.NewSliceEventReader(events)
}

type modelsCapableHandler struct {
	modelsOut   exchange.ListModelsOutput
	modelsErr   error
	gotModelsIn exchange.ListModelsInput
}

func (h *modelsCapableHandler) HandleRequest(_ context.Context, _ exchange.RequestInput) (exchange.RequestOutput, error) {
	return exchange.RequestOutput{Response: exchange.NewTransportResponseFromDocument(testDocumentFromOutput(
		canonical.ClientFamilyChatCompletions,
		canonical.NewConversationOutput(
			"chatcmpl_1",
			"m",
			[]canonical.OutputItem{
				canonical.NewTextOutputItem("text_0", "ok"),
			},
			"stop",
		),
	))}, nil
}

func synthesizeRequestOutputFromEnvelope(in exchange.RequestInput, envelope canonical.EventReader) (exchange.RequestOutput, error) {
	request, clientDelivery := mustDecodeCapturedRequest(in)
	doc, err := buildRequestDocumentForTest(in)
	if err != nil {
		return exchange.RequestOutput{}, err
	}
	clientFamily := canonical.ClientFamily(doc.Family)
	if clientDelivery.Mode == delivery.Streaming {
		stream, err := testResponseStreamEncoderForFamily(clientFamily).EncodeResponseStream(envelope, clientDelivery)
		if err != nil {
			return exchange.RequestOutput{}, err
		}
		return exchange.RequestOutput{Response: exchange.NewTransportResponseFromStream(stream, false)}, nil
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), envelope, canonical.EnvResponse)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return exchange.RequestOutput{}, canonical.InternalError("buffered provider response envelope ended before response closure")
		}
		return exchange.RequestOutput{}, err
	}
	output, err := closed.ProjectResponse()
	if err != nil {
		return exchange.RequestOutput{}, err
	}
	_ = request
	responseDoc := testDocumentFromOutput(clientFamily, output)
	return exchange.RequestOutput{Response: exchange.NewTransportResponseFromDocument(responseDoc)}, nil
}

func testDecodeCapturedRequest(t *testing.T, in exchange.RequestInput) canonical.CanonicalRequest {
	t.Helper()
	request, _, err := decodeCapturedRequest(in)
	if err != nil {
		t.Fatalf("decode captured request: %v", err)
	}
	return request
}

func mustDecodeCapturedRequest(in exchange.RequestInput) (canonical.CanonicalRequest, delivery.Delivery) {
	request, clientDelivery, err := decodeCapturedRequest(in)
	if err != nil {
		panic(err)
	}
	return request, clientDelivery
}

func decodeCapturedRequest(in exchange.RequestInput) (canonical.CanonicalRequest, delivery.Delivery, error) {
	doc, err := buildRequestDocumentForTest(in)
	if err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
	}
	switch canonical.ClientFamily(doc.Family) {
	case canonical.ClientFamilyChatCompletions:
		result, err := chatcompletions.ClientRequestDecoder{}.DecodeClientRequest(doc)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		return result.Value.Request, normalizeStreamingDeliveryForTest(result.Value.Delivery, in.ResponseFraming), nil
	case canonical.ClientFamilyResponses:
		result, err := responses.ClientRequestDecoder{}.DecodeClientRequest(doc)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		return result.Value.Request, normalizeStreamingDeliveryForTest(result.Value.Delivery, in.ResponseFraming), nil
	case canonical.ClientFamilyCompletions:
		result, err := completions.ClientRequestDecoder{}.DecodeClientRequest(doc)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		return result.Value.Request, normalizeStreamingDeliveryForTest(result.Value.Delivery, in.ResponseFraming), nil
	case canonical.ClientFamilyMessages:
		result, err := messages.ClientRequestDecoder{}.DecodeClientRequest(doc)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		return result.Value.Request, normalizeStreamingDeliveryForTest(result.Value.Delivery, in.ResponseFraming), nil
	default:
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), errors.New("unsupported captured request family")
	}
}

func buildRequestDocumentForTest(in exchange.RequestInput) (carrier.WireDocument, error) {
	raw, err := io.ReadAll(in.Request.Body)
	if err != nil {
		return carrier.WireDocument{}, err
	}
	_ = in.Request.Body.Close()
	normalizedPath, err := canonical.NormalizePath(in.Request.URL)
	if err != nil {
		return carrier.WireDocument{}, err
	}
	family := in.ClientFamily
	if family == "" {
		hasMessagesProtocolMarker := strings.TrimSpace(in.Request.Header.Get("anthropic-version")) != "" // swobu:io-string source=boundary
		family, err = canonical.InferClientFamily(in.Request.Method, normalizedPath, hasMessagesProtocolMarker)
		if err != nil {
			return carrier.WireDocument{}, err
		}
	}
	return carrier.NewWireDocument(carrier.StageClientRequestIn, family, "application/json", in.Request.Header, raw, carrier.Meta{}), nil
}

func replicateRequestInputForTest(in exchange.RequestInput, copies int) ([]exchange.RequestInput, error) {
	raw, err := io.ReadAll(in.Request.Body)
	if err != nil {
		return nil, err
	}
	_ = in.Request.Body.Close()
	header := in.Request.Header.Clone()
	out := make([]exchange.RequestInput, 0, copies)
	for range copies {
		out = append(out, exchange.RequestInput{
			EndpointName:    in.EndpointName,
			Request:         exchange.NewTransportRequest(in.Request.Method, in.Request.URL, header, raw),
			ClientFamily:    in.ClientFamily,
			ResponseFraming: in.ResponseFraming,
			ExchangeID:      in.ExchangeID,
		})
	}
	return out, nil
}

func normalizeStreamingDeliveryForTest(clientDelivery delivery.Delivery, framing delivery.Framing) delivery.Delivery {
	if clientDelivery.Mode != delivery.Streaming || clientDelivery.Framing != delivery.FramingNone || framing == delivery.FramingNone {
		return clientDelivery
	}
	return delivery.StreamingDelivery(framing)
}

func testDocumentFromOutput(family canonical.ClientFamily, output canonical.CanonicalOutput) carrier.WireDocument {
	doc, err := testResponseDocumentEncoderForFamily(family).EncodeResponseDocument(output)
	if err != nil {
		panic(err)
	}
	return doc
}

type responseDocumentEncoderForTest struct {
	encode func(canonical.CanonicalOutput) (carrier.WireDocument, error)
}

func (e responseDocumentEncoderForTest) EncodeResponseDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	return e.encode(output)
}

func testResponseDocumentEncoderForFamily(family canonical.ClientFamily) responseDocumentEncoderForTest {
	switch family {
	case canonical.ClientFamilyChatCompletions:
		return responseDocumentEncoderForTest{encode: func(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
			result, err := chatcompletions.ResponseDocumentEncoder{}.EncodeResponseDocument(output)
			return result.Value, err
		}}
	case canonical.ClientFamilyResponses:
		return responseDocumentEncoderForTest{encode: func(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
			result, err := responses.ResponseDocumentEncoder{}.EncodeResponseDocument(output)
			return result.Value, err
		}}
	case canonical.ClientFamilyCompletions:
		return responseDocumentEncoderForTest{encode: func(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
			result, err := completions.ResponseDocumentEncoder{}.EncodeResponseDocument(output)
			return result.Value, err
		}}
	case canonical.ClientFamilyMessages:
		return responseDocumentEncoderForTest{encode: func(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
			result, err := messages.ResponseDocumentEncoder{}.EncodeResponseDocument(output)
			return result.Value, err
		}}
	default:
		panic("test response document encoder missing for family " + string(family))
	}
}

type responseStreamEncoderForTest struct {
	encode func(canonical.EventReader, delivery.Delivery) (carrier.WireStream, error)
}

func (e responseStreamEncoderForTest) EncodeResponseStream(events canonical.EventReader, d delivery.Delivery) (carrier.WireStream, error) {
	return e.encode(events, d)
}

func testResponseStreamEncoderForFamily(family canonical.ClientFamily) responseStreamEncoderForTest {
	switch family {
	case canonical.ClientFamilyChatCompletions:
		return responseStreamEncoderForTest{encode: func(events canonical.EventReader, d delivery.Delivery) (carrier.WireStream, error) {
			result, err := chatcompletions.ResponseStreamEncoder{}.EncodeResponseStream(events, d)
			return result.Value, err
		}}
	case canonical.ClientFamilyResponses:
		return responseStreamEncoderForTest{encode: func(events canonical.EventReader, d delivery.Delivery) (carrier.WireStream, error) {
			result, err := responses.ResponseStreamEncoder{}.EncodeResponseStream(events, d)
			return result.Value, err
		}}
	case canonical.ClientFamilyCompletions:
		return responseStreamEncoderForTest{encode: func(events canonical.EventReader, d delivery.Delivery) (carrier.WireStream, error) {
			result, err := completions.ResponseStreamEncoder{}.EncodeResponseStream(events, d)
			return result.Value, err
		}}
	case canonical.ClientFamilyMessages:
		return responseStreamEncoderForTest{encode: func(events canonical.EventReader, d delivery.Delivery) (carrier.WireStream, error) {
			result, err := messages.ResponseStreamEncoder{}.EncodeResponseStream(events, d)
			return result.Value, err
		}}
	default:
		panic("test response stream encoder missing for family " + string(family))
	}
}

func (h *modelsCapableHandler) ListModels(_ context.Context, in exchange.ListModelsInput) (exchange.ListModelsOutput, error) {
	h.gotModelsIn = in
	return h.modelsOut, h.modelsErr
}

type writeHeaderCountingResponseWriter struct {
	header                http.Header
	statusCode            int
	writeHeaderCount      int
	writeCount            int
	cancelAfterWriteCount int
	cancel                func()
}

func (w *writeHeaderCountingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *writeHeaderCountingResponseWriter) WriteHeader(statusCode int) {
	w.writeHeaderCount++
	w.statusCode = statusCode
}

func (w *writeHeaderCountingResponseWriter) Write(p []byte) (int, error) {
	w.writeCount++
	if w.cancelAfterWriteCount > 0 && w.writeCount == w.cancelAfterWriteCount && w.cancel != nil {
		w.cancel()
	}
	return len(p), nil
}

type firstChunkThenErrorBody struct {
	readCount int
}

func (b *firstChunkThenErrorBody) Read(p []byte) (int, error) {
	if b.readCount == 0 {
		b.readCount++
		p[0] = 'x'
		return 1, nil
	}
	return 0, errors.New("stream body failed")
}

func (b *firstChunkThenErrorBody) Close() error { return nil }

type immediateReadErrorBody struct{}

func (immediateReadErrorBody) Read([]byte) (int, error) { return 0, errors.New("stream body failed") }

func (immediateReadErrorBody) Close() error { return nil }
