package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/search"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The generated SearchRoute contract requires a JSON array, including [] for
// a single Connection. Null or omission must fail before any command dispatch.
func TestSearchRouteRejectsMissingOrNullFallbackArray(t *testing.T) {
	handler := NewNodeControlAPIHandler(workspaces.Service{})
	for _, body := range []string{`{"connection_id":"cn_primary"}`, `{"connection_id":"cn_primary","fallback_connection_ids":null}`} {
		req := httptest.NewRequest("PUT", "/api/v1/workspaces/research/search-route", strings.NewReader(body))
		req = permissionOperatorRequest(req)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != 400 {
			t.Fatalf("missing/null fallback array status=%d want=400", res.Code)
		}
	}
}

func TestSearchEvidenceProjectsRetentionFailureWithoutHidingLiveFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "search.json")
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := search.OpenEvidenceStore(path, 2)
	if !errors.Is(err, search.ErrEvidenceRetention) {
		t.Fatal(err)
	}
	store.Append(search.Evidence{Operation: "live", Outcome: "started"})
	req := permissionOperatorRequest(httptest.NewRequest("GET", "/api/v1/search-evidence", nil))
	res := httptest.NewRecorder()
	NewSearchEvidenceHandler(store).ServeHTTP(res, req)
	var payload struct {
		Events         []search.Evidence `json:"events"`
		RetentionError bool              `json:"retention_error"`
	}
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &payload) != nil || !payload.RetentionError || len(payload.Events) != 1 {
		t.Fatalf("retention/live projection status=%d body=%s", res.Code, res.Body)
	}
}
