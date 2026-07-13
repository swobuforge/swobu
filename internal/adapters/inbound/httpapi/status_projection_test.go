package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	trafficevidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/trafficevidence"
)

func TestStatusProjectionHandler_RejectsMissingScope(t *testing.T) {
	handler := NewStatusProjectionHandler(func(context.Context, trafficevidencestore.ProjectionScope) (trafficevidencestore.StatusProjection, error) {
		t.Fatal("read should not be called when scope is missing")
		return trafficevidencestore.StatusProjection{}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/_swobu/status-projection", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if got := w.Code; got != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", got, http.StatusBadRequest)
	}
}

func TestStatusProjectionHandler_RejectsInvalidScope(t *testing.T) {
	handler := NewStatusProjectionHandler(func(context.Context, trafficevidencestore.ProjectionScope) (trafficevidencestore.StatusProjection, error) {
		t.Fatal("read should not be called when scope is invalid")
		return trafficevidencestore.StatusProjection{}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/_swobu/status-projection?scope=workspace:acme", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if got := w.Code; got != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", got, http.StatusBadRequest)
	}
}

func TestStatusProjectionHandler_PassesAllScope(t *testing.T) {
	called := false
	handler := NewStatusProjectionHandler(func(_ context.Context, scope trafficevidencestore.ProjectionScope) (trafficevidencestore.StatusProjection, error) {
		called = true
		if scope.Kind != trafficevidencestore.ProjectionScopeAll {
			t.Fatalf("scope kind = %q, want %q", scope.Kind, trafficevidencestore.ProjectionScopeAll)
		}
		return trafficevidencestore.StatusProjection{
			Scope: scope,
			Counters: trafficevidencestore.StatusCounters{
				PerModel: map[string]int{},
			},
		}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/_swobu/status-projection?scope=all", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !called {
		t.Fatal("read function was not called")
	}
	if got := w.Code; got != http.StatusOK {
		t.Fatalf("status = %d, want %d", got, http.StatusOK)
	}
}

func TestStatusProjectionHandler_PassesEndpointScope(t *testing.T) {
	called := false
	handler := NewStatusProjectionHandler(func(_ context.Context, scope trafficevidencestore.ProjectionScope) (trafficevidencestore.StatusProjection, error) {
		called = true
		if scope.Kind != trafficevidencestore.ProjectionScopeEndpoint {
			t.Fatalf("scope kind = %q, want %q", scope.Kind, trafficevidencestore.ProjectionScopeEndpoint)
		}
		if scope.Endpoint != "acme" {
			t.Fatalf("scope endpoint = %q, want %q", scope.Endpoint, "acme")
		}
		return trafficevidencestore.StatusProjection{
			Scope: scope,
			Counters: trafficevidencestore.StatusCounters{
				PerModel: map[string]int{},
			},
		}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/_swobu/status-projection?scope=endpoint:acme", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !called {
		t.Fatal("read function was not called")
	}
	if got := w.Code; got != http.StatusOK {
		t.Fatalf("status = %d, want %d", got, http.StatusOK)
	}
}
