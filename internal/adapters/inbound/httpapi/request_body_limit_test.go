package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
)

func TestOperatorJSONHandlersRejectOversizeBeforeApplicationBehavior(t *testing.T) {
	padding := bytes.Repeat([]byte(" "), int(maxOperatorJSONBodyBytes)+1)

	t.Run("workspace command", func(t *testing.T) {
		body := append([]byte(`{"slug":"dev","initial_route":"chat","target":{"id":"a","model":"gpt-5","protocol":"responses","connection":{"openai":{"credential":"env:OPENAI_API_KEY"}}}}`), padding...)
		recorder := httptest.NewRecorder()
		NewWorkspaceControlHandler(workspaceapi.Service{}).ServeHTTP(
			recorder,
			httptest.NewRequest(http.MethodPost, "/_swobu/workspaces", bytes.NewReader(body)),
		)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want bounded rejection", recorder.Code)
		}
	})

	t.Run("auth session", func(t *testing.T) {
		called := false
		handler := NewAuthSessionHandler(func(context.Context, authplane.StartInput) (authplane.StartOutput, error) {
			called = true
			return authplane.StartOutput{}, nil
		}, nil, nil, nil)
		body := append([]byte(`{"provider_spec":"chatgpt","auth_mode":"browser"}`), padding...)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/_swobu/auth/sessions", bytes.NewReader(body)))
		if recorder.Code != http.StatusBadRequest || called {
			t.Fatalf("status=%d application_called=%t, want bounded rejection", recorder.Code, called)
		}
	})

	t.Run("target probe", func(t *testing.T) {
		stub := &stubTargetProber{}
		raw, err := json.Marshal(targetProbeRequest{Connection: workspaceapi.StandardConnection("openai", "", "env:OPENAI_API_KEY"), ProviderProtocol: "responses"})
		if err != nil {
			t.Fatal(err)
		}
		body := append(raw, padding...)
		recorder := httptest.NewRecorder()
		NewTargetProbeHandler(stub).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/_swobu/target-probe", bytes.NewReader(body)))
		if recorder.Code != http.StatusBadRequest || len(stub.attempts) != 0 {
			t.Fatalf("status=%d provider_attempts=%d, want bounded rejection", recorder.Code, len(stub.attempts))
		}
	})
}
