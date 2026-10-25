package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/admission"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestWorkspaceOperationalHTTPCountsModelsAndSearchButNotMCPHandshake(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("research")
	limits := routing.ServiceLimits{
		Models: routing.ServiceQuota{RequestsPerMinute: 1},
		MCP:    routing.ServiceQuota{RequestsPerMinute: 1},
		Search: routing.ServiceQuota{RequestsPerMinute: 1},
	}
	limiter := admission.New(func(routing.WorkspaceSlug) (string, routing.ServiceLimits, error) {
		return "lifetime", limits, nil
	})
	calls := 0
	handler := NewLocalAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}), nil, limiter.Admit)
	request := func(path string) *httptest.ResponseRecorder {
		r := admittedRequest()
		r.Method = http.MethodPost
		r.URL.Path = path
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}
	for _, path := range []string{"/workspaces/research/v1/responses", "/workspaces/research/search"} {
		if got := request(path); got.Code != http.StatusNoContent {
			t.Fatalf("initial %s = %d", path, got.Code)
		}
		got := request(path)
		if got.Code != http.StatusTooManyRequests || got.Header().Get("Retry-After") == "" {
			t.Fatalf("exhausted %s = %d retry=%q", path, got.Code, got.Header().Get("Retry-After"))
		}
	}
	for range 3 {
		if got := request("/workspaces/research/mcp"); got.Code != http.StatusNoContent {
			t.Fatalf("MCP setup blocked: %d", got.Code)
		}
	}
	// An actual MCP tool invocation is charged at the dispatch boundary.
	if _, _, err := limiter.Admit(slug, routing.ServiceMCP); err != nil {
		t.Fatal("MCP handshake spent operational tool credit:", err)
	}
	if _, _, err := limiter.Admit(slug, routing.ServiceMCP); err != admission.ErrExhausted {
		t.Fatal("MCP tool credit was not enforced:", err)
	}
	if calls != 5 {
		t.Fatalf("unexpected number of admitted requests: %d", calls)
	}
}

func TestWorkspaceModelConcurrencyReleasesAfterDelivery(t *testing.T) {
	limits := routing.ServiceLimits{Models: routing.ServiceQuota{Concurrent: 1}}
	limiter := admission.New(func(routing.WorkspaceSlug) (string, routing.ServiceLimits, error) {
		return "lifetime", limits, nil
	})
	entered, finish := make(chan struct{}), make(chan struct{})
	handler := NewLocalAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-entered:
		default:
			close(entered)
			<-finish
		}
		w.WriteHeader(http.StatusNoContent)
	}), nil, limiter.Admit)
	request := func() *httptest.ResponseRecorder {
		r := admittedRequest()
		r.Method = http.MethodPost
		r.URL.Path = "/workspaces/research/v1/responses"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- request() }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first request did not enter")
	}
	if got := request(); got.Code != http.StatusTooManyRequests {
		t.Fatalf("concurrent request = %d", got.Code)
	}
	close(finish)
	select {
	case got := <-firstDone:
		if got.Code != http.StatusNoContent {
			t.Fatalf("first delivery = %d", got.Code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first request never released")
	}
	if got := request(); got.Code != http.StatusNoContent {
		t.Fatalf("slot not returned after delivery: %d", got.Code)
	}
}
