package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
)

func TestChatGPTLoginHandlerGenericStart(t *testing.T) {
	t.Parallel()
	handler := NewAuthSessionHandler(
		func(_ context.Context, in authplane.StartInput) (authplane.StartOutput, error) {
			if in.ProviderSpec != "chatgpt" {
				t.Fatalf("provider spec = %q", in.ProviderSpec)
			}
			if in.EndpointRef != "main/chatgpt" {
				t.Fatalf("endpoint ref = %q", in.EndpointRef)
			}
			return authplane.StartOutput{
				SessionID:    "sess-g1",
				AuthorizeURL: "https://login.example",
				State:        authplane.SessionStatePending,
			}, nil
		},
		nil,
		nil,
		nil,
	)
	req := httptest.NewRequest(http.MethodPost, "/_swobu/auth/sessions", strings.NewReader(`{"provider_spec":"chatgpt","endpoint_ref":"main/chatgpt"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
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

func TestChatGPTLoginHandlerGenericSession(t *testing.T) {
	t.Parallel()
	handler := NewAuthSessionHandler(
		nil,
		func(_ context.Context, sessionID string) (authplane.SessionOutput, error) {
			if sessionID != "sess-g1" {
				t.Fatalf("session id = %q", sessionID)
			}
			return authplane.SessionOutput{
				ProviderSpec:  "chatgpt",
				SessionID:     "sess-g1",
				State:         authplane.SessionStateSucceeded,
				CredentialRef: "keychain:chatgpt/default",
			}, nil
		},
		nil,
		nil,
	)
	req := httptest.NewRequest(http.MethodGet, "/_swobu/auth/sessions/sess-g1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
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
		nil,
		func(_ context.Context, sessionID string) error {
			if sessionID != "sess-g1" {
				t.Fatalf("session id = %q", sessionID)
			}
			return nil
		},
		nil,
	)
	req := httptest.NewRequest(http.MethodPost, "/_swobu/auth/sessions/sess-g1/cancel", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestChatGPTLoginHandlerGenericRetry(t *testing.T) {
	t.Parallel()
	handler := NewAuthSessionHandler(
		nil,
		nil,
		nil,
		func(_ context.Context, sessionID string) (authplane.StartOutput, error) {
			if sessionID != "sess-g1" {
				t.Fatalf("session id = %q", sessionID)
			}
			return authplane.StartOutput{
				SessionID:    "sess-g2",
				AuthorizeURL: "https://login.example/new",
				State:        authplane.SessionStatePending,
			}, nil
		},
	)
	req := httptest.NewRequest(http.MethodPost, "/_swobu/auth/sessions/sess-g1/retry", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
