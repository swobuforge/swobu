package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Control transport rejects successful JSON without no-store. This shared
// writer is used by Workspace list/get and service lifecycle projections.
func TestWorkspaceControlJSONAlwaysNoStore(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusConflict, http.StatusPreconditionFailed} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			response := httptest.NewRecorder()
			writeWorkspaceJSON(response, status, map[string]any{"ok": true})
			if response.Code != status || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("response=%d headers=%v", response.Code, response.Header())
			}
			if !strings.Contains(response.Body.String(), `"ok":true`) {
				t.Fatal("missing JSON payload")
			}
		})
	}
}
