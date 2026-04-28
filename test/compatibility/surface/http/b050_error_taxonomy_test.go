package http_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metrofun/swobu/internal/adapters/inbound/httpapi"
	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/ports"
)

func TestB050_WireErrorTaxonomy(t *testing.T) {
	t.Run("swobu error renders canonical envelope", func(t *testing.T) {
		handler := httpapi.NewHandler(fakeRequestHandler{
			err: compatibility.UnsupportedOperation("selected target cannot satisfy request"),
		})
		req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		if body := rec.Body.String(); !bytes.Contains([]byte(body), []byte(`"code":"UNSUPPORTED_OPERATION"`)) {
			t.Fatalf("body = %q, want structured unsupported operation error", body)
		}
		if body := rec.Body.String(); !bytes.Contains([]byte(body), []byte(`"origin":"swobu"`)) {
			t.Fatalf("body = %q, want swobu origin", body)
		}
	})

	t.Run("backend error preserves status body and retry-after", func(t *testing.T) {
		handler := httpapi.NewHandler(fakeRequestHandler{
			err: compatibility.NewBackendError("backend-a", 429, `{"error":"rate limited"}`, "30"),
		})
		req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
		}
		if got := rec.Header().Get("Retry-After"); got != "30" {
			t.Fatalf("retry-after = %q, want %q", got, "30")
		}
		if body := rec.Body.String(); body != `{"error":"rate limited"}` {
			t.Fatalf("body = %q, want backend body passthrough", body)
		}
	})

	t.Run("internal ingress fault renders typed internal error", func(t *testing.T) {
		handler := httpapi.NewHandler(nil)
		req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
		}
		if body := rec.Body.String(); !bytes.Contains([]byte(body), []byte(`"code":"INTERNAL_ERROR"`)) {
			t.Fatalf("body = %q, want structured internal error", body)
		}
	})

	t.Run("responses conversation selector is rejected explicitly", func(t *testing.T) {
		handler := httpapi.NewHandler(fakeRequestHandler{
			err: compatibility.BadRequest("responses conversation is not supported in swobu v0"),
		})
		req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","conversation":"conv_123","input":"continue"}`))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
		if body := rec.Body.String(); !bytes.Contains([]byte(body), []byte(`responses conversation is not supported in swobu v0`)) {
			t.Fatalf("body = %q, want explicit conversation rejection", body)
		}
	})

	t.Run("responses missing previous_response_id fails explicitly on the public wire", func(t *testing.T) {
		endpoint := contractTestEndpoint(t)
		handler := httpapi.NewHandler(requestpath.NewRequestHandler(
			contractEndpointReader{endpoint: endpoint},
			&panicProviderExecutor{},
			nil,
			nil,
		))
		req := httptest.NewRequest(http.MethodPost, "/c/alpha/responses", bytes.NewBufferString(`{"model":"m","previous_response_id":"resp_missing","input":"continue"}`))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
		body := rec.Body.String()
		if !bytes.Contains([]byte(body), []byte(`"code":"BAD_REQUEST"`)) {
			t.Fatalf("body = %q, want BAD_REQUEST envelope", body)
		}
		if !bytes.Contains([]byte(body), []byte(`responses previous_response_id could not be rehydrated`)) {
			t.Fatalf("body = %q, want explicit local missing-parent truth", body)
		}
	})

	t.Run("gzip request content coding is accepted", func(t *testing.T) {
		handler := httpapi.NewHandler(fakeRequestHandler{
			out: requestpath.HandleOutput{
				Response: fakeExecuteResponse(204, "", compatibility.DeliveryModeBuffered),
			},
		})
		var encoded bytes.Buffer
		gz := gzip.NewWriter(&encoded)
		_, _ = gz.Write([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
		_ = gz.Close()
		req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewReader(encoded.Bytes()))
		req.Header.Set("Content-Encoding", "gzip")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}

type fakeRequestHandler struct {
	out requestpath.HandleOutput
	err error
}

func (h fakeRequestHandler) Handle(_ context.Context, _ requestpath.HandleInput) (requestpath.HandleOutput, error) {
	return h.out, h.err
}

func fakeExecuteResponse(status int, body string, mode compatibility.DeliveryMode) ports.ExecuteResponse {
	_ = status
	_ = body
	switch mode {
	case compatibility.DeliveryModeStreaming:
		return ports.NewStreamingExecuteResponse(compatibility.NewSliceEventStream([]compatibility.OutputEvent{
			{Kind: compatibility.OutputEventStarted, ResultID: "chatcmpl_1", Model: "m"},
			{Kind: compatibility.OutputEventCompleted, FinishReason: "stop"},
		}))
	default:
		return ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"chatcmpl_1",
				"m",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				},
				"stop",
			),
		)
	}
}

type contractEndpointReader struct {
	endpoint endpointintent.Endpoint
}

func (r contractEndpointReader) GetEndpoint(context.Context, endpointintent.EndpointName) (endpointintent.Endpoint, error) {
	return r.endpoint, nil
}

type panicProviderExecutor struct{}

func (panicProviderExecutor) Execute(context.Context, ports.ExecuteRequest) (ports.ExecuteResponse, error) {
	panic("provider executor must not be called for local missing-parent failure")
}

func contractTestEndpoint(t *testing.T) endpointintent.Endpoint {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(
		ref,
		spec,
		"https://example.test/v1",
		"",
		protocolsurface.Responses,
	)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithModelID("m")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{providerConfig}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}
