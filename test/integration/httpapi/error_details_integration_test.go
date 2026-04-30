package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metrofun/swobu/internal/adapters/inbound/httpapi"
	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
)

func TestHTTPAPI_TypedSwobuErrorPreservesDetailsEnvelope(t *testing.T) {
	t.Parallel()

	handler := httpapi.NewHandler(errorDetailStaticRequestHandler{
		err: compatibility.Error{
			Code:    compatibility.ErrorCodeUnknownTarget,
			Message: "target selector not found",
			Origin:  compatibility.ErrorOriginSwobu,
			Details: map[string]string{
				"selector": "blue",
				"hint":     "use model=primary or /models",
			},
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Post(server.URL+"/c/acme/chat/completions", "application/json", bytes.NewBufferString(`{"model":"blue","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("POST returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var envelope struct {
		Error struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Origin  string            `json:"origin"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if envelope.Error.Code != string(compatibility.ErrorCodeUnknownTarget) {
		t.Fatalf("code = %q, want %q", envelope.Error.Code, compatibility.ErrorCodeUnknownTarget)
	}
	if envelope.Error.Message != "target selector not found" {
		t.Fatalf("message = %q, want target selector not found", envelope.Error.Message)
	}
	if envelope.Error.Origin != string(compatibility.ErrorOriginSwobu) {
		t.Fatalf("origin = %q, want %q", envelope.Error.Origin, compatibility.ErrorOriginSwobu)
	}
	if envelope.Error.Details["selector"] != "blue" {
		t.Fatalf("details.selector = %q, want blue", envelope.Error.Details["selector"])
	}
	if envelope.Error.Details["hint"] != "use model=primary or /models" {
		t.Fatalf("details.hint = %q, want recovery hint", envelope.Error.Details["hint"])
	}
}

type errorDetailStaticRequestHandler struct {
	out requestpath.HandleOutput
	err error
}

func (h errorDetailStaticRequestHandler) Handle(_ context.Context, _ requestpath.HandleInput) (requestpath.HandleOutput, error) {
	return h.out, h.err
}
