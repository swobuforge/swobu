package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadOnlyGateRefusesMutationBeforeHandler(t *testing.T) {
	called := 0
	gate := readOnlyNodeControlGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(http.StatusAccepted) }))
	table := []struct {
		method, path string
		blocked      bool
	}{
		{"POST", "/api/v1/connections", true}, {"PUT", "/api/v1/workspaces/research/limits", true},
		{"DELETE", "/api/v1/principals/someone", true},
		{"POST", "/_swobu/workspaces", false}, {"POST", "/_swobu/credential-files", false},
		{"POST", "/_swobu/access", false},
		{"GET", "/api/v1/connections", false}, {"GET", "/api/v1/workspaces", false},
		{"POST", "/workspaces/research/v1/responses", false}, {"POST", "/workspaces/research/mcp", false},
		{"POST", "/api/v1/daemon/shutdown", false}, {"PUT", "/api/v1/daemon/shutdown", true},
		{"DELETE", "/api/v1/daemon/shutdown", true}, {"GET", "/api/v1/telemetry/report", false},
	}
	for _, tt := range table {
		t.Run(tt.method+"_"+strings.ReplaceAll(tt.path, "/", "_"), func(t *testing.T) {
			before := called
			w := httptest.NewRecorder()
			r := httptest.NewRequest(tt.method, tt.path, nil)
			gate.ServeHTTP(w, r)
			if tt.blocked {
				if w.Code != http.StatusConflict || called != before || !strings.Contains(w.Body.String(), "config_read_only") {
					t.Fatalf("blocked=%d called=%d body=%s", w.Code, called, w.Body.String())
				}
			}
			if !tt.blocked {
				if w.Code != http.StatusAccepted || called != before+1 {
					t.Fatalf("unexpected rejection: %d", w.Code)
				}
			}
		})
	}
}
