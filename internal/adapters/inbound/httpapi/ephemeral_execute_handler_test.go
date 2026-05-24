package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
)

func TestEphemeralExecuteHandler_ExecutesWithDecodedEndpoint(t *testing.T) {
	called := false
	h := NewEphemeralExecuteHandler(func(_ context.Context, endpoint endpointintent.Endpoint, in requestpath.HandleInput) (requestpath.HandleOutput, error) {
		called = true
		if got := endpoint.Name().String(); got != "ephemeral-probe" {
			t.Fatalf("endpoint name = %q, want ephemeral-probe", got)
		}
		dialog, ok := in.Request.(canonical.DialogCanonicalRequest)
		if !ok {
			t.Fatalf("request type = %T, want canonical.DialogCanonicalRequest", in.Request)
		}
		if got := dialog.Model(); got != "m-test" {
			t.Fatalf("probe model = %q, want m-test", got)
		}
		return requestpath.HandleOutput{}, nil
	})

	body := map[string]any{
		"endpoint": map[string]any{
			"name":                         "ephemeral-probe",
			"selected_provider_config_ref": "selected",
			"provider_configs": []map[string]any{{
				"ref":            "selected",
				"provider_spec":  "openai",
				"credential_ref": "env:OPENAI_API_KEY",
				"model_id":       "m-test",
			}},
		},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/_swobu/ephemeral-executions", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !called {
		t.Fatal("execute was not called")
	}
}

func TestEphemeralExecuteHandler_PropagatesRawError(t *testing.T) {
	h := NewEphemeralExecuteHandler(func(context.Context, endpointintent.Endpoint, requestpath.HandleInput) (requestpath.HandleOutput, error) {
		return requestpath.HandleOutput{}, errors.New("BAD_ENDPOINT: broken auth")
	})
	body := `{"endpoint":{"name":"ephemeral-probe","selected_provider_config_ref":"selected","provider_configs":[{"ref":"selected","provider_spec":"openai","credential_ref":"env:OPENAI_API_KEY","model_id":"m-test"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/_swobu/ephemeral-executions", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["error"] != "BAD_ENDPOINT: broken auth" {
		t.Fatalf("error = %v, want raw backend error", got["error"])
	}
}
