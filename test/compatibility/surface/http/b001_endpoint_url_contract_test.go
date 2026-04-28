package http_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metrofun/swobu/internal/adapters/inbound/httpapi"
	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/ports"
)

func TestB001_EndpointURLContract(t *testing.T) {
	handler := httpapi.NewHandler(fakeRequestHandler{
		out: requestpath.HandleOutput{
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
		},
	})

	t.Run("base path without redirect", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/c/alpha", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code == http.StatusMovedPermanently || rec.Code == http.StatusPermanentRedirect || rec.Code == http.StatusTemporaryRedirect {
			t.Fatalf("unexpected redirect status %d", rec.Code)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("base path with trailing slash without redirect", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/c/alpha/", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code == http.StatusMovedPermanently || rec.Code == http.StatusPermanentRedirect || rec.Code == http.StatusTemporaryRedirect {
			t.Fatalf("unexpected redirect status %d", rec.Code)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("unknown endpoint returns structured bad endpoint", func(t *testing.T) {
		badHandler := httpapi.NewHandler(fakeRequestHandler{
			err: compatibility.BadEndpoint("endpoint could not be resolved"),
		})
		req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
		rec := httptest.NewRecorder()

		badHandler.ServeHTTP(rec, req)

		if rec.Code != 502 {
			t.Fatalf("status = %d, want %d", rec.Code, 502)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q, want %q", got, "application/json")
		}
		if body := rec.Body.String(); !bytes.Contains([]byte(body), []byte(`"code":"BAD_ENDPOINT"`)) {
			t.Fatalf("body = %q, want BAD_ENDPOINT envelope", body)
		}
	})
}
