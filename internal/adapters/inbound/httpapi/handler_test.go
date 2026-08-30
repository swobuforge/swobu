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
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/testkit/providertest"
	chatcompletions "github.com/swobuforge/swobu/internal/wire/chatcompletions"
	messages "github.com/swobuforge/swobu/internal/wire/messages"
	responses "github.com/swobuforge/swobu/internal/wire/responses"
)

func newTestHandler(ingress requestIngress) Handler {
	return NewHandler(ingress, nil)
}

func TestLogRequestOutcomeUsesDeliveryOwnedClientCancellation(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	logRequestOutcome("request-a", "personal", canonical.ClientFamilyMessages,
		canonical.NormalizedPathMessages, "target-a", 1,
		delivery.Result{Kind: delivery.ClientCancelled, Err: errors.New("private downstream write error")})

	entries := decodeHTTPLogEntries(t, logs.Bytes())
	if len(entries) != 1 {
		t.Fatalf("log entries = %#v, want one", entries)
	}
	entry := entries[0]
	if entry["result"] != "canceled" || entry["status_code"] != float64(clientClosedRequestStatus) || entry["error_origin"] != "client" {
		t.Fatalf("client cancellation log = %#v", entry)
	}
	if _, ok := entry["error_code"]; ok {
		t.Fatalf("client cancellation acquired error_code: %#v", entry)
	}
	if strings.Contains(logs.String(), "private downstream write error") {
		t.Fatalf("request outcome exposed downstream error prose: %s", logs.String())
	}
}

func decodeHTTPLogEntries(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var entries []map[string]any
	for decoder.More() {
		var entry map[string]any
		if err := decoder.Decode(&entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func TestHandler_ForwardsCanonicalRequest(t *testing.T) {
	capturing := &capturingRequestIngress{}
	handler := newTestHandler(capturing)
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
	if got, want := capturing.got.ClientHandler, trafficevidence.NormalizeClientHandler("Codex/1.2"); got != want {
		t.Fatalf("client handler = %q, want %q", got, want)
	}
}

func TestHandler_ThreadsTimingLifecycleThroughResponseCommit(t *testing.T) {
	ingress := &timingCaptureIngress{}
	handler := newTestHandler(ingress)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("User-Agent", "Codex/1.2")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ingress.got.Timing == nil {
		t.Fatal("request timing was not threaded into ingress")
	}
	if started, ok := ingress.got.Timing.StartedAt(); !ok || started.IsZero() {
		t.Fatalf("request timing started_at = (%v,%v), want started", started, ok)
	}
	if started, ok := ingress.got.Timing.StartedAt(); !ok || started.IsZero() {
		t.Fatalf("commit timing started_at = (%v,%v), want started", started, ok)
	}
	if first, ok := ingress.got.Timing.FirstByteAt(); !ok || first.IsZero() {
		t.Fatalf("commit timing first_byte_at = (%v,%v), want first byte", first, ok)
	}
	if ended, ok := ingress.got.Timing.EndedAt(); !ok || ended.IsZero() {
		t.Fatalf("commit timing ended_at = (%v,%v), want ended", ended, ok)
	}
	started, _ := ingress.got.Timing.StartedAt()
	first, _ := ingress.got.Timing.FirstByteAt()
	ended, _ := ingress.got.Timing.EndedAt()
	if first.Before(started) {
		t.Fatalf("first_byte_at = %v, want at or after started_at = %v", first, started)
	}
	if ended.Before(first) {
		t.Fatalf("ended_at = %v, want at or after first_byte_at = %v", ended, first)
	}
}

func TestHandler_LogsClientProvenanceOnSuccessAndError(t *testing.T) {
	setDefaultLogger, logs := testDebugLogger()
	defer setDefaultLogger()

	success := newTestHandler(staticRequestIngress{
		envelope: testProviderIngressFromOutput(
			canonicaltest.Response(t,
				"chatcmpl_1",
				"m",
				[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok")},
				canonical.Completed("stop"),
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

	fail := newTestHandler(staticRequestIngress{
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
		"target_id=openai",
		"status_code=502",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("logs missing %q\nlogs:\n%s", want, out)
		}
	}
	if strings.Contains(out, "provider failed") || strings.Contains(out, "backend_error_detail") {
		t.Fatalf("backend response content reached ordinary logs:\n%s", out)
	}
	if body := recFail.Body.String(); body != `{"error":"provider failed"}` {
		t.Fatalf("backend response body = %q", body)
	}
}

func TestExchangeFailureDeliveryResult_PreservesClientCancellation(t *testing.T) {
	result := exchangeFailureDeliveryResult(provider.Cancelled(context.Canceled))
	if result.Kind != delivery.ClientCancelled {
		t.Fatalf("delivery kind = %q, want client cancellation", result.Kind)
	}
	if got := statusCodeForExchangeError(result.Err); got != clientClosedRequestStatus {
		t.Fatalf("cancellation status = %d, want %d", got, clientClosedRequestStatus)
	}
}

func TestHandler_ProjectsProviderTimeoutAcrossClientFamilies(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		body string
	}{
		{name: "Messages", path: "/c/alpha/messages", body: `{"model":"m","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`},
		{name: "Responses", path: "/c/alpha/responses", body: `{"model":"m","input":"hello"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := newTestHandler(staticRequestIngress{err: canonical.ProviderTimeout("provider did not respond before the configured deadline")})
			request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
			if test.name == "Messages" {
				request.Header.Set("anthropic-version", "2023-06-01")
			}
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusGatewayTimeout {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusGatewayTimeout, response.Body.String())
			}
			var envelope swobuErrorEnvelope
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error.Code != string(canonical.ErrorCodeProviderTimeout) {
				t.Fatalf("error code = %q, want %q", envelope.Error.Code, canonical.ErrorCodeProviderTimeout)
			}
		})
	}
}

func TestWriteExchangeErrorDefaultsStatuslessBackendFailureToBadGateway(t *testing.T) {
	recorder := httptest.NewRecorder()
	err := canonical.NewBackendError("responses", 0, "provider contract failed", "")

	writeExchangeError(recorder, err)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if body := recorder.Body.String(); body != "provider contract failed" {
		t.Fatalf("body = %q", body)
	}
	if got := statusCodeForExchangeError(err); got != http.StatusBadGateway {
		t.Fatalf("traffic status = %d, want %d", got, http.StatusBadGateway)
	}
}

func TestHandler_LogsSwobuErrorDetailsOnFailure(t *testing.T) {
	setDefaultLogger, logs := testDebugLogger()
	defer setDefaultLogger()

	handler := newTestHandler(staticRequestIngress{
		out: exchange.RequestOutput{Target: provider.TargetSnapshot{TargetID: "selected-chat"}},
		err: canonical.ClientUnsupportedOperation(
			"chat completions endpoint does not support namespace tool declarations",
			"Change the tool declaration to function or custom and retry",
		),
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
		"target_id=selected-chat",
		"error_code=UNSUPPORTED_OPERATION",
		"status_code=400",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("logs missing %q\nlogs:\n%s", want, out)
		}
	}
	if strings.Contains(out, "namespace tool declarations") {
		t.Fatalf("public error message reached logs:\n%s", out)
	}
}

func TestHandler_LogsResponsesToolReferenceDetailsOnFailure(t *testing.T) {
	setDefaultLogger, logs := testDebugLogger()
	defer setDefaultLogger()

	handler := newTestHandler(staticRequestIngress{
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
		"status_code=400",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("logs missing %q\nlogs:\n%s", want, out)
		}
	}
	if strings.Contains(out, "exec_command__bogus") || strings.Contains(out, "tools[].name") {
		t.Fatalf("request-derived error detail reached logs:\n%s", out)
	}
}

// TestHandler_ClassifiesUntypedInternalFailureAsInternalError covers the real
// production path that previously produced an anonymous 500: a terminal failure
// that is NOT a typed canonical.Error (e.g. a bare fmt.Errorf from tool
// preparation). The terminal classifier must label it error_code=INTERNAL_ERROR
// on the request_outcome line, while the client body stays generic and the raw
// cause never reaches either surface.
func TestHandler_ClassifiesUntypedInternalFailureAsInternalError(t *testing.T) {
	setDefaultLogger, logs := testDebugLogger()
	defer setDefaultLogger()

	// A plain untyped error is what an unguarded internal failure looks like.
	// Embed values that must never reach the log line or the client body.
	const secretFragment = "AKIA-SECRET-TOKEN"
	const toolName = "exec_command__bogus"
	untyped := errors.New("provider request preparation failed at tool=" + toolName + " token=" + secretFragment)
	handler := newTestHandler(staticRequestIngress{err: untyped})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","input":"ping"}`))
	req.Header.Set("User-Agent", "Codex/1.0")
	req.Header.Set("X-Request-Id", "req_untyped")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failure status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	out := logs.String()
	for _, want := range []string{
		"event=request_outcome",
		"request_id=req_untyped",
		"result=swobu_error",
		"error_origin=swobu",
		"error_code=INTERNAL_ERROR",
		"status_code=500",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("logs missing %q\nlogs:\n%s", want, out)
		}
	}
	// Log privacy: the raw cause and any request-derived fragments it carries
	// must not reach the compact request_outcome line.
	for _, leak := range []string{secretFragment, toolName, "provider request preparation failed"} {
		if strings.Contains(out, leak) {
			t.Fatalf("internal cause reached logs (%q):\n%s", leak, out)
		}
	}
	// Client-body privacy: the body is the generic envelope, never the cause.
	body := rec.Body.String()
	for _, leak := range []string{secretFragment, toolName, "provider request preparation failed"} {
		if strings.Contains(body, leak) {
			t.Fatalf("internal cause reached client body (%q):\n%s", leak, body)
		}
	}
	if !strings.Contains(body, `"code":"INTERNAL_ERROR"`) || !strings.Contains(body, `"message":"internal server error"`) {
		t.Fatalf("client body is not the generic INTERNAL_ERROR envelope:\n%s", body)
	}
}

func TestHandler_ServesEndpointModels(t *testing.T) {
	handler := newTestHandler(&modelsCapableHandler{
		modelsOut: exchange.ListModelsOutput{
			DefaultModelID: "custom:gpt-4o",
			Models: []exchange.ModelOption{
				{ID: "custom:gpt-4o"},
				{ID: "custom:gpt-4.1"},
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
	if !strings.Contains(body, `"name":"custom:gpt-4o"`) {
		t.Fatalf("body = %q, want model name", body)
	}
	if !strings.Contains(body, `"id":"default"`) || !strings.Contains(body, `"name":"default"`) {
		t.Fatalf("body = %q, want public default-route model", body)
	}
	if strings.Contains(body, `"swobu_model"`) || strings.Contains(body, `"swobu_default"`) || strings.Contains(body, `"swobu_backend"`) || strings.Contains(body, `"swobu_provider"`) {
		t.Fatalf("body = %q, want OpenAI-shaped model entries without swobu_* fields", body)
	}
}

func TestHandler_ServesDefaultThenLexicalModelsWithoutSchemaExpansion(t *testing.T) {
	handler := newTestHandler(&modelsCapableHandler{modelsOut: exchange.ListModelsOutput{
		DefaultModelID: "chat",
		Models:         []exchange.ModelOption{{ID: "chat"}, {ID: "claude-fast"}},
	}})
	var previous string
	for attempt := 0; attempt < 3; attempt++ {
		req := httptest.NewRequest(http.MethodGet, "/c/alpha/v1/models?limit=1000", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		var body struct {
			Object string           `json:"object"`
			Data   []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		wantIDs := []string{"default", "chat", "claude-fast"}
		if body.Object != "list" || len(body.Data) != len(wantIDs) {
			t.Fatalf("response = %#v, want list with %d entries", body, len(wantIDs))
		}
		for index, entry := range body.Data {
			if entry["id"] != wantIDs[index] || entry["name"] != wantIDs[index] {
				t.Fatalf("entry %d = %#v, want id/name %q", index, entry, wantIDs[index])
			}
			if len(entry) != 5 {
				t.Fatalf("entry %d expanded schema: %#v", index, entry)
			}
		}
		if previous != "" && rec.Body.String() != previous {
			t.Fatalf("response changed between calls:\nfirst: %s\nnext: %s", previous, rec.Body.String())
		}
		previous = rec.Body.String()
	}
}

func TestHandler_MissingWorkspaceRequestsAndModelsReturnSlugSpecificBadEndpoint(t *testing.T) {
	missing := canonical.BadEndpoint(`Workspace "default" does not exist. Create it in Swobu or check the workspace name in this endpoint.`)
	for _, tc := range []struct {
		name    string
		method  string
		path    string
		body    string
		ingress *modelsCapableHandler
	}{
		{
			name: "request", method: http.MethodPost, path: "/c/default/responses",
			body:    `{"model":"gpt-5.3-codex","input":"hi"}`,
			ingress: &modelsCapableHandler{requestErr: missing},
		},
		{
			name: "models", method: http.MethodGet, path: "/c/default/v1/models",
			ingress: &modelsCapableHandler{modelsErr: missing},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := newTestHandler(tc.ingress)
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			body := rec.Body.String()
			for _, want := range []string{`"code":"BAD_ENDPOINT"`, "default", "does not exist", "Create it in Swobu"} {
				if !strings.Contains(body, want) {
					t.Fatalf("body %q missing %q", body, want)
				}
			}
			if strings.Contains(body, `"object":"list"`) || strings.Contains(body, `"id":"default"`) || strings.Contains(body, `"output"`) {
				t.Fatalf("missing workspace returned synthetic success payload: %s", body)
			}
		})
	}
}

func TestHandler_ServesEndpointModelsAliasPath(t *testing.T) {
	handler := newTestHandler(&modelsCapableHandler{
		modelsOut: exchange.ListModelsOutput{
			DefaultModelID: "custom:gpt-4o",
			Models:         []exchange.ModelOption{{ID: "custom:gpt-4o"}},
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
	if !strings.Contains(rec.Body.String(), `"name":"custom:gpt-4o"`) {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestHandler_RejectsNonGETModelsRequests(t *testing.T) {
	handler := newTestHandler(&modelsCapableHandler{})
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
		canonicaltest.MustResponse(
			"chatcmpl_1",
			"resolved-model",
			[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok")},
			canonical.Completed("stop"),
		),
	)
	handler := newTestHandler(staticRequestIngress{
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
	handler := newTestHandler(capturing)
	var encoded bytes.Buffer
	gz := gzip.NewWriter(&encoded)
	_, _ = gz.Write([]byte(`{"model":"m","tools":[{"name":"calc","description":"calculate","input_schema":{"type":"object"}}],"messages":[{"role":"assistant","content":[{"type":"text","text":"working"},{"type":"tool_use","id":"toolu_1","name":"calc","input":{"expr":"2+2"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"4"}]}]}`))
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
	if len(items) != 4 {
		t.Fatalf("items len = %d, want declarations plus 3 history items", len(items))
	}
	if got := items[2].Kind(); got != canonical.ItemKindToolCall {
		t.Fatalf("item kind = %q, want %q", got, canonical.ItemKindToolCall)
	}
	message, _ := items[1].Message()
	if got := message.Role(); got != canonical.MessageRoleAssistant {
		t.Fatalf("author = %q, want %q", got, canonical.MessageRoleAssistant)
	}
	if got := items[3].Kind(); got != canonical.ItemKindToolResult {
		t.Fatalf("item kind = %q, want %q", got, canonical.ItemKindToolResult)
	}
}

func TestHandler_RejectsOversizedRequestBody(t *testing.T) {
	capturing := &capturingRequestIngress{}
	handler := newTestHandler(capturing)
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
	handler := newTestHandler(capturing)

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
	handler := newTestHandler(capturing)
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
	message, _ := items[0].Message()
	text, _ := message.Content()[0].Text()
	if text.Text() != "hi" {
		t.Fatalf("item text = %q, want %q", text.Text(), "hi")
	}
}

func TestHandler_PreservesResponsesStateAndStructuredInput(t *testing.T) {
	capturing := &capturingRequestIngress{}
	handler := newTestHandler(capturing)
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","previous_response_id":"resp_123","prompt_cache_key":"repo-alpha","tools":[{"type":"function","name":"grep","parameters":{"type":"object"}}],"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},{"type":"function_call","call_id":"call_1","name":"grep","arguments":{"pattern":"TODO"}},{"type":"function_call_output","call_id":"call_1","output":"2 hits"}]}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	typed := testDecodeCapturedRequest(t, capturing.got)
	if got, ok := typed.PreviousResponse(); !ok || got.SwobuID.String() != "resp_123" {
		t.Fatalf("previous_response = %#v, want resp_123", got)
	}
	items := typed.Items()
	if len(items) != 4 {
		t.Fatalf("conversation len = %d, want declarations plus 3 history items", len(items))
	}
	if got := items[2].Kind(); got != canonical.ItemKindToolCall {
		t.Fatalf("item kind = %q, want %q", got, canonical.ItemKindToolCall)
	}
	if got := items[3].Kind(); got != canonical.ItemKindToolResult {
		t.Fatalf("item kind = %q, want %q", got, canonical.ItemKindToolResult)
	}
}

func TestHandler_DecodesResponsesToolChoiceStrictIntoCanonicalToolPolicy(t *testing.T) {
	capturing := &capturingRequestIngress{}
	handler := newTestHandler(capturing)
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
	handler := newTestHandler(capturing)
	functionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "grep"), "search text", canonicaltest.MustSchema(`{"type":"object","properties":{"pattern":{"type":"string"}}}`), canonical.Unspecified[bool]())
	projectedFunctionName := providertest.ProjectedToolName(t, functionTool)
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
	wantSpecific := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "grep").String()
	if specific, ok := typed.ToolPolicy().SpecificID(); !ok || specific.String() != wantSpecific {
		t.Fatalf("tool policy specific = %q, want %q", specific, wantSpecific)
	}
}

func TestHandler_RejectsResponsesRequestsWithBothPreviousResponseSelectors(t *testing.T) {
	capturing := &capturingRequestIngress{}
	handler := newTestHandler(capturing)
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
	handler := newTestHandler(capturing)
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
	handler := newTestHandler(&capturingRequestIngress{})
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
	handler := newTestHandler(&capturingRequestIngress{})
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
	handler := newTestHandler(staticRequestIngress{
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
		if !json.Valid([]byte(message)) {
			t.Fatalf("websocket message is not one JSON event: %q", message)
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
	if !strings.Contains(joined, `"item_id":"item_0"`) {
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
	handler := newTestHandler(staticRequestIngress{
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
	handler := newTestHandler(&capturingRequestIngress{})
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
	handler := newTestHandler(&capturingRequestIngress{})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/messages", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}]}]}`))
	req.Header.Set("anthropic-version", "2023-06-01")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if body := rec.Body.String(); !strings.Contains(body, "image source is invalid") {
		t.Fatalf("body = %q, want invalid image source failure", body)
	}
}

func TestHandler_EncodesToolCallStreamingForResponses(t *testing.T) {
	handler := newTestHandler(staticRequestIngress{
		envelope: testStreamingToolResponse("resp_1", "m", "tool_0", "call_1", "grep", []string{`{"pattern":"TO`, `DO"}`}, "completed"),
	})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","input":"hi","tools":[{"type":"function","name":"grep","parameters":{"type":"object"}}],"stream":true}`))
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
	handler := newTestHandler(staticRequestIngress{
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

	handler := newTestHandler(staticRequestIngress{
		out: exchange.RequestOutput{
			Response: exchange.StreamingResponse{Response: carrier.Response{
				Status: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
				},
				Body: &firstChunkThenErrorBody{},
			}},
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

	handler := newTestHandler(staticRequestIngress{
		out: exchange.RequestOutput{
			Response: exchange.StreamingResponse{Response: carrier.Response{
				Status: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
				},
				Body: &firstChunkThenErrorBody{},
			}},
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
	handler := newTestHandler(staticRequestIngress{
		envelope: testStreamingToolResponse("msg_1", "m", "tool_0", "call_1", "grep", []string{`{"pattern":"TODO"}`}, "tool_use"),
	})
	req := httptest.NewRequest(http.MethodPost, "/c/alpha/messages", bytes.NewBufferString(`{"model":"m","tools":[{"name":"grep","description":"search","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hi"}],"stream":true}`))
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

func (h *capturingRequestIngress) HandleRequest(ctx context.Context, in exchange.RequestInput) (exchange.RequestOutput, error) {
	clones, err := replicateRequestInputForTest(in, 3)
	if err != nil {
		return exchange.RequestOutput{}, err
	}
	h.got = clones[0]
	if _, _, err := decodeCapturedRequest(clones[1]); err != nil {
		return exchange.RequestOutput{}, err
	}
	out, err := synthesizeRequestOutputFromEnvelope(ctx, clones[2], testProviderIngressFromOutput(
		canonicaltest.MustResponse(
			"chatcmpl_1",
			"m",
			[]canonical.CanonicalItem{
				canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok"),
			},
			canonical.Completed("stop"),
		),
	))
	if err != nil {
		return exchange.RequestOutput{}, err
	}
	out.Target = provider.NewCustomTargetSnapshot("backend-a", "https://example.test/v1", "cred-1", protocolkind.ChatCompletions, "chat_completions", "Authorization", delivery.BufferedDelivery())
	return out, nil
}

type timingCaptureIngress struct {
	got exchange.RequestInput
}

func (h *timingCaptureIngress) HandleRequest(_ context.Context, in exchange.RequestInput) (exchange.RequestOutput, error) {
	h.got = in
	return exchange.RequestOutput{
		Response: exchange.BufferedResponse{Response: carrier.Response{
			Status: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: io.NopCloser(bytes.NewReader([]byte(`{}`))),
		}},
	}, nil
}

type staticRequestIngress struct {
	out      exchange.RequestOutput
	err      error
	envelope canonical.ResponseStream
}

func (h staticRequestIngress) HandleRequest(ctx context.Context, in exchange.RequestInput) (exchange.RequestOutput, error) {
	if h.envelope != nil {
		return synthesizeRequestOutputFromEnvelope(ctx, in, h.envelope)
	}
	return h.out, h.err
}

func testProviderIngressFromOutput(output canonical.CanonicalResponse) canonical.ResponseStream {
	return canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		"test_buffered:httpapi",
		output.Response(),
		output.Model(),
		output.Items(),
		output.Completion(),
		output.Usage(),
	))
}

func testStreamingEmptyResponse() canonical.ResponseStream {
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

func testStreamingTextResponse(resultID string, model string, itemID string, text string, finish string) canonical.ResponseStream {
	item := canonicaltest.MustMessage(canonical.MessageRoleAssistant, text)
	events := canonical.EventSequence{
		{ExchangeID: "test_exchange", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Model: model}},
		{ExchangeID: "test_exchange", Seq: 2, Kind: canonical.EventResponseIdentity, EnvID: "res_1", Payload: canonical.ResponseIdentityPayload{Response: canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID(resultID)}}},
		{ExchangeID: "test_exchange", Seq: 3, Kind: canonical.EventItemStart, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonicaltest.MustMessageStart(canonical.MessageRoleAssistant)}, Meta: canonical.EventMetadataFields{NativeID: itemID}},
		{ExchangeID: "test_exchange", Seq: 4, Kind: canonical.EventTextDelta, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonical.TextDeltaPayload{Text: text}}},
		{ExchangeID: "test_exchange", Seq: 5, Kind: canonical.EventItemCompleted, EnvID: "msg_1", ParentID: "res_1", Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonical.ItemCompletedPayload{Item: item}}},
		{ExchangeID: "test_exchange", Seq: 6, Kind: canonical.EventFinish, EnvID: "res_1", Payload: canonical.FinishPayload{Completion: canonical.Completed(finish)}},
		{ExchangeID: "test_exchange", Seq: 7, Kind: canonical.EventEnvelopeEnd, EnvID: "res_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	}
	return canonical.NewSliceEventReader(events)
}

func testStreamingToolResponse(resultID string, model string, itemID string, toolUseID string, name string, argDeltas []string, finish string) canonical.ResponseStream {
	callID, err := canonical.NewToolCallID(toolUseID)
	if err != nil {
		panic(err)
	}
	toolID := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, name)
	tool := toolID
	events := canonical.EventSequence{
		{ExchangeID: "test_exchange", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Model: model}},
		{ExchangeID: "test_exchange", Seq: 2, Kind: canonical.EventResponseIdentity, EnvID: "res_1", Payload: canonical.ResponseIdentityPayload{Response: canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID(resultID)}}},
		{ExchangeID: "test_exchange", Seq: 3, Kind: canonical.EventItemStart, EnvID: "tool_1", ParentID: "res_1", Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonicaltest.MustToolCallStart(callID, tool)}, Meta: canonical.EventMetadataFields{NativeID: itemID}},
	}
	seq := int64(4)
	arguments := ""
	for _, delta := range argDeltas {
		arguments += delta
		events = append(events, canonical.Event{ExchangeID: "test_exchange", Seq: seq, Kind: canonical.EventArgsDelta, EnvID: "tool_1", ParentID: "res_1", Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonical.ArgsDeltaPayload{Args: delta}}})
		seq++
	}
	object, err := canonical.ParseJSONObject([]byte(arguments))
	if err != nil {
		panic(err)
	}
	item, err := canonical.NewToolCallItem(callID, tool, canonical.NewJSONObjectToolInput(object))
	if err != nil {
		panic(err)
	}
	events = append(events,
		canonical.Event{ExchangeID: "test_exchange", Seq: seq, Kind: canonical.EventItemCompleted, EnvID: "tool_1", ParentID: "res_1", Payload: canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonical.ItemCompletedPayload{Item: item}}},
		canonical.Event{ExchangeID: "test_exchange", Seq: seq + 1, Kind: canonical.EventFinish, EnvID: "res_1", Payload: canonical.FinishPayload{Completion: canonical.Completed(finish)}},
		canonical.Event{ExchangeID: "test_exchange", Seq: seq + 2, Kind: canonical.EventEnvelopeEnd, EnvID: "res_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	)
	return canonical.NewSliceEventReader(events)
}

type modelsCapableHandler struct {
	modelsOut   exchange.ListModelsOutput
	modelsErr   error
	requestErr  error
	gotModelsIn exchange.ListModelsInput
}

func (h *modelsCapableHandler) HandleRequest(_ context.Context, _ exchange.RequestInput) (exchange.RequestOutput, error) {
	if h.requestErr != nil {
		return exchange.RequestOutput{}, h.requestErr
	}
	return exchange.RequestOutput{Response: exchange.NewBufferedResponse(testDocumentFromOutput(
		canonical.ClientFamilyChatCompletions,
		canonicaltest.MustResponse(
			"chatcmpl_1",
			"m",
			[]canonical.CanonicalItem{
				canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok"),
			},
			canonical.Completed("stop"),
		),
	))}, nil
}

func synthesizeRequestOutputFromEnvelope(ctx context.Context, in exchange.RequestInput, envelope canonical.ResponseStream) (exchange.RequestOutput, error) {
	_, clientDelivery := mustDecodeCapturedRequest(in)
	doc, err := buildRequestDocumentForTest(in)
	if err != nil {
		return exchange.RequestOutput{}, err
	}
	clientFamily := canonical.ClientFamily(doc.Family)
	if clientDelivery.Mode == delivery.Streaming {
		if clientDelivery.Framing == delivery.FramingWebSocket {
			result, err := responses.ResponseStreamEncoder{}.EncodeResponseMessages(ctx, canonical.CanonicalRequest{}, envelope, clientDelivery)
			if err != nil {
				return exchange.RequestOutput{}, err
			}
			return exchange.RequestOutput{Response: exchange.NewMessageStreamingResponse(result.Response, result.Completion)}, nil
		}
		stream, err := testResponseStreamEncoderForFamily(clientFamily).EncodeResponseStream(ctx, envelope, clientDelivery)
		if err != nil {
			return exchange.RequestOutput{}, err
		}
		return exchange.RequestOutput{Response: exchange.NewStreamingResponse(stream, nil)}, nil
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
	responseDoc := testDocumentFromOutput(clientFamily, *output)
	return exchange.RequestOutput{Response: exchange.NewBufferedResponse(responseDoc)}, nil
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
		return result.Request.Request, normalizeStreamingDeliveryForTest(result.Request.Delivery, in.ResponseFraming), nil
	case canonical.ClientFamilyResponses:
		result, err := responses.ClientRequestDecoder{}.DecodeClientRequest(doc)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		return result.Request.Request, normalizeStreamingDeliveryForTest(result.Request.Delivery, in.ResponseFraming), nil
	case canonical.ClientFamilyMessages:
		result, err := messages.ClientRequestDecoder{}.DecodeClientRequest(doc)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		return result.Request.Request, normalizeStreamingDeliveryForTest(result.Request.Delivery, in.ResponseFraming), nil
	default:
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), errors.New("unsupported captured request family")
	}
}

func buildRequestDocumentForTest(in exchange.RequestInput) (carrier.Document, error) {
	normalizedPath, err := canonical.NormalizePath(in.Request.URL)
	if err != nil {
		return carrier.Document{}, err
	}
	family := in.ClientFamily
	if family == "" {
		hasMessagesProtocolMarker := strings.TrimSpace(in.Request.Header.Get("anthropic-version")) != "" // swobu:io-string source=boundary
		family, err = canonical.InferClientFamily(in.Request.Method, normalizedPath, hasMessagesProtocolMarker)
		if err != nil {
			return carrier.Document{}, err
		}
	}
	return carrier.NewDocument(family, "application/json", in.Request.Header, in.Request.Body, carrier.Meta{}), nil
}

func replicateRequestInputForTest(in exchange.RequestInput, copies int) ([]exchange.RequestInput, error) {
	header := in.Request.Header.Clone()
	out := make([]exchange.RequestInput, 0, copies)
	for range copies {
		out = append(out, exchange.RequestInput{
			Workspace:       in.Workspace,
			Request:         exchange.NewTransportRequest(in.Request.Method, in.Request.URL, header, in.Request.Body),
			ClientHandler:   in.ClientHandler,
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

func testDocumentFromOutput(family canonical.ClientFamily, output canonical.CanonicalResponse) carrier.Document {
	doc, err := testResponseDocumentEncoderForFamily(family).EncodeResponseDocument(output)
	if err != nil {
		panic(err)
	}
	return doc
}

type responseDocumentEncoderForTest struct {
	encode func(canonical.CanonicalResponse) (carrier.Document, error)
}

func (e responseDocumentEncoderForTest) EncodeResponseDocument(output canonical.CanonicalResponse) (carrier.Document, error) {
	return e.encode(output)
}

func testResponseDocumentEncoderForFamily(family canonical.ClientFamily) responseDocumentEncoderForTest {
	switch family {
	case canonical.ClientFamilyChatCompletions:
		return responseDocumentEncoderForTest{encode: func(output canonical.CanonicalResponse) (carrier.Document, error) {
			result, err := chatcompletions.ResponseDocumentEncoder{}.EncodeResponseDocument(canonical.CanonicalRequest{}, output)
			return result.Document, err
		}}
	case canonical.ClientFamilyResponses:
		return responseDocumentEncoderForTest{encode: func(output canonical.CanonicalResponse) (carrier.Document, error) {
			result, err := responses.ResponseDocumentEncoder{}.EncodeResponseDocument(canonical.CanonicalRequest{}, output)
			return result.Document, err
		}}
	case canonical.ClientFamilyMessages:
		return responseDocumentEncoderForTest{encode: func(output canonical.CanonicalResponse) (carrier.Document, error) {
			result, err := messages.ResponseDocumentEncoder{}.EncodeResponseDocument(canonical.CanonicalRequest{}, output)
			return result.Document, err
		}}
	default:
		panic("test response document encoder missing for family " + string(family))
	}
}

type responseStreamEncoderForTest struct {
	encode func(context.Context, canonical.ResponseStream, delivery.Delivery) (carrier.ByteStream, error)
}

func (e responseStreamEncoderForTest) EncodeResponseStream(ctx context.Context, events canonical.ResponseStream, d delivery.Delivery) (carrier.ByteStream, error) {
	return e.encode(ctx, events, d)
}

func testResponseStreamEncoderForFamily(family canonical.ClientFamily) responseStreamEncoderForTest {
	switch family {
	case canonical.ClientFamilyChatCompletions:
		return responseStreamEncoderForTest{encode: func(ctx context.Context, events canonical.ResponseStream, d delivery.Delivery) (carrier.ByteStream, error) {
			result, err := chatcompletions.ResponseStreamEncoder{}.EncodeResponseStream(ctx, canonical.CanonicalRequest{}, events, d)
			return result.Stream, err
		}}
	case canonical.ClientFamilyResponses:
		return responseStreamEncoderForTest{encode: func(ctx context.Context, events canonical.ResponseStream, d delivery.Delivery) (carrier.ByteStream, error) {
			result, err := responses.ResponseStreamEncoder{}.EncodeResponseStream(ctx, canonical.CanonicalRequest{}, events, d)
			return result.Stream, err
		}}
	case canonical.ClientFamilyMessages:
		return responseStreamEncoderForTest{encode: func(ctx context.Context, events canonical.ResponseStream, d delivery.Delivery) (carrier.ByteStream, error) {
			result, err := messages.ResponseStreamEncoder{}.EncodeResponseStream(ctx, canonical.CanonicalRequest{}, events, d)
			return result.Stream, err
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
