package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
)

func TestChatGPTLoginHandlerGenericSession(t *testing.T) {
	t.Parallel()
	handler := NewAuthSessionHandler(
		func(_ context.Context, sessionID string) (authplane.SessionOutput, error) {
			if sessionID != "sess-g1" {
				t.Fatalf("session id = %q", sessionID)
			}
			return authplane.SessionOutput{
				ProviderSpec:  "chatgpt",
				SessionID:     "sess-g1",
				State:         authplane.SessionStateSucceeded,
				CredentialRef: "secret:chatgpt/default",
			}, nil
		}, nil,
	)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth-sessions/sess-g1", nil)
	rec := httptest.NewRecorder()
	localAuthorityTestHandler(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["provider_spec"] != "chatgpt" {
		t.Fatalf("provider_spec = %v", body["provider_spec"])
	}
}

func TestChatGPTLoginHandlerGenericCancel(t *testing.T) {
	t.Parallel()
	handler := NewAuthSessionHandler(
		nil,
		func(_ context.Context, sessionID string) error {
			if sessionID != "sess-g1" {
				t.Fatalf("session id = %q", sessionID)
			}
			return nil
		},
	)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/auth-sessions/sess-g1", nil)
	rec := httptest.NewRecorder()
	localAuthorityTestHandler(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
