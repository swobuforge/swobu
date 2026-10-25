package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
)

type authStartDriver struct{ starts *int }

func (d authStartDriver) Start(context.Context, authplane.StartInput) (authplane.DriverStartResult, error) {
	*d.starts++
	return authplane.DriverStartResult{SessionID: "sess-1", AuthorizeURL: "https://example.test", ExpiresAt: "2026-10-14T00:00:00Z"}, nil
}
func (authStartDriver) Poll(context.Context, string) (authplane.DriverPollResult, error) {
	return authplane.DriverPollResult{}, nil
}
func (authStartDriver) Cancel(context.Context, string) error { return nil }

func TestCanonicalAuthStartRetainsBeforeDispatchAndReopen(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "attempts")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, step := range []string{"missing", "created", "reopened", "changed", "query", "denied"} {
		attempts, err := authplane.OpenStartAttempts(directory)
		if err != nil {
			t.Fatal(err)
		}
		manager, err := authplane.NewAuthSessionManager(authStartDriver{&starts})
		if err != nil {
			t.Fatal(err)
		}
		handler := NewAuthSessionHandler(manager.Poll, manager.Cancel).WithStartAttempts(manager, attempts)
		body := `{"provider_spec":"chatgpt","draft_subject":"original","auth_mode":"browser"}`
		if step == "changed" {
			body = strings.Replace(body, "original", "changed", 1)
		}
		path := "/api/v1/auth-sessions"
		if step == "query" {
			path += "?dry_run=true"
		}
		request := operatorJSONTestRequest(http.MethodPost, path, body)
		if step != "missing" {
			request.Header.Set("Idempotency-Key", "original-start-key")
		}
		recorder := httptest.NewRecorder()
		if step == "denied" {
			handler.ServeHTTP(recorder, request)
		} else {
			localAuthorityTestHandler(handler).ServeHTTP(recorder, request)
		}
		want := map[string]int{"missing": 400, "created": 201, "reopened": 409, "changed": 409, "query": 404, "denied": 403}[step]
		if recorder.Code != want {
			t.Fatalf("%s status=%d body=%s", step, recorder.Code, recorder.Body.String())
		}
		if step == "created" && recorder.Header().Get("Location") != "/api/v1/auth-sessions/sess-1" {
			t.Fatal("missing location")
		}
	}
	if starts != 1 {
		t.Fatalf("starts=%d", starts)
	}
}
