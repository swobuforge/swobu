package httpapi

import (
	"context"
	"errors"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
)

func TestCanonicalAuthSessionObservationAndCancellation(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			handler := NewAuthSessionHandler(func(_ context.Context, id string) (authplane.SessionOutput, error) {
				calls++
				if id != "sess-1" {
					t.Fatal(id)
				}
				return authplane.SessionOutput{SessionID: id, State: authplane.SessionStatePending}, nil
			}, func(_ context.Context, id string) error {
				calls++
				if id != "sess-1" {
					t.Fatal(id)
				}
				return nil
			})
			request := httptest.NewRequest(method, "/api/v1/auth-sessions/sess-1", nil)
			recorder := httptest.NewRecorder()
			localAuthorityTestHandler(handler).ServeHTTP(recorder, request)
			if recorder.Code != 200 || calls != 1 || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d calls=%d headers=%v", recorder.Code, calls, recorder.Header())
			}
		})
	}
}

func TestCanonicalAuthSessionRejectsMalformedPathsAndAuthority(t *testing.T) {
	calls := 0
	handler := NewAuthSessionHandler(func(context.Context, string) (authplane.SessionOutput, error) {
		calls++
		return authplane.SessionOutput{}, nil
	}, func(context.Context, string) error { calls++; return nil })
	for _, path := range []string{"/api/v1/auth-sessions/", "/api/v1/auth-sessions/sess-1/", "/api/v1/auth-sessions/a/b", "/api/v1/auth-sessions/%73ess-1", "/api/v1/auth-sessions/a%2fb", "/api/v1/auth-sessions/..", "/api/v1/auth-sessions/sess-1?dry_run=true", "/_swobu/auth/sessions/sess-1", "/_swobu/auth/sessions"} {
		recorder := httptest.NewRecorder()
		localAuthorityTestHandler(handler).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code == 200 {
			t.Fatalf("accepted %s", path)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/auth-sessions/sess-1", nil))
	if recorder.Code != 403 || calls != 0 {
		t.Fatalf("unadmitted calls=%d status=%d", calls, recorder.Code)
	}
}

func TestLegacyAuthStartAndRetryPathsDoNotDispatch(t *testing.T) {
	handler := NewAuthSessionHandler(nil, nil)
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/_swobu/auth/sessions", strings.NewReader(`{"provider_spec":"chatgpt","draft_subject":"original","auth_mode":"browser"}`)),
		httptest.NewRequest(http.MethodPost, "/_swobu/auth/sessions/sess-1/retry", strings.NewReader(`{}`)),
	} {
		recorder := httptest.NewRecorder()
		localAuthorityTestHandler(handler).ServeHTTP(recorder, request)
		if recorder.Code == http.StatusOK || recorder.Code == http.StatusCreated || recorder.Code == http.StatusConflict || recorder.Code == http.StatusBadRequest {
			t.Fatalf("legacy path %s remained an auth operation: %d", request.URL.Path, recorder.Code)
		}
	}
}

func TestCanonicalAuthCancellationRejectsPayloadAndWithdrawnAdmission(t *testing.T) {
	for _, withdrawn := range []bool{false, true} {
		calls := 0
		handler := NewAuthSessionHandler(nil, func(context.Context, string) error { calls++; return nil })
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodDelete, "/api/v1/auth-sessions/sess-1", nil)
		if withdrawn {
			admitted := true
			request = request.WithContext(contextWithAuthority(request, func(routing.WorkspaceSlug) (accessidentity.ID, error) {
				if !admitted {
					return accessidentity.ID{}, errors.New("withdrawn")
				}
				return accessidentity.Local, nil
			}))
			request.Body = io.NopCloser(authCancellationBody{read: func() { admitted = false }})
			handler.ServeHTTP(recorder, request)
			if recorder.Code != 403 {
				t.Fatalf("withdrawn status=%d", recorder.Code)
			}
		} else {
			request.Body = io.NopCloser(strings.NewReader("{}"))
			localAuthorityTestHandler(handler).ServeHTTP(recorder, request)
			if recorder.Code != 400 {
				t.Fatalf("payload status=%d", recorder.Code)
			}
		}
		if calls != 0 {
			t.Fatal("invalid cancellation dispatched")
		}
	}
}

type authCancellationBody struct{ read func() }

func (b authCancellationBody) Read([]byte) (int, error) { b.read(); return 0, io.EOF }
