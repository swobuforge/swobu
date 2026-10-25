package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
)

func TestOperatorJSONHandlersRejectOversizeBeforeApplicationBehavior(t *testing.T) {
	padding := bytes.Repeat([]byte(" "), int(maxOperatorJSONBodyBytes)+1)

	t.Run("workspace command", func(t *testing.T) {
		body := append([]byte(`{"slug":"dev","initial_route":"chat","target":{"id":"a","model":"gpt-5","protocol":"responses","connection_id":"cn_account"}}`), padding...)
		recorder := httptest.NewRecorder()
		request := permissionOperatorRequest(httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader(body)))
		request.Header.Set("Idempotency-Key", "oversize-fixture")
		request.Header.Set("Content-Type", "application/json")
		NewNodeControlAPIHandler(workspaceapi.Service{}).ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want bounded rejection", recorder.Code)
		}
	})

	t.Run("auth session", func(t *testing.T) {
		manager, err := authplane.NewAuthSessionManager(testAuthMethodDriver{})
		if err != nil {
			t.Fatal(err)
		}
		directory := filepath.Join(t.TempDir(), "auth-attempts")
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		attempts, err := authplane.OpenStartAttempts(directory)
		if err != nil {
			t.Fatal(err)
		}
		handler := NewAuthSessionHandler(manager.Poll, manager.Cancel).WithStartAttempts(manager, attempts)
		body := append([]byte(`{"provider_spec":"chatgpt","auth_mode":"browser"}`), padding...)
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth-sessions", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		localAuthorityTestHandler(handler).ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want bounded rejection", recorder.Code)
		}
	})

}

type testAuthMethodDriver struct{}

func (testAuthMethodDriver) Start(context.Context, authplane.StartInput) (authplane.DriverStartResult, error) {
	return authplane.DriverStartResult{}, nil
}
func (testAuthMethodDriver) Poll(context.Context, string) (authplane.DriverPollResult, error) {
	return authplane.DriverPollResult{}, nil
}
func (testAuthMethodDriver) Cancel(context.Context, string) error { return nil }
