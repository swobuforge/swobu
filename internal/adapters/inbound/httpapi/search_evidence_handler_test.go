package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/search"
)

func TestSearchEvidenceHandlerRequiresLocalOperatorAdmission(t *testing.T) {
	store := search.NewEvidenceStore(4)
	store.Append(search.Evidence{Operation: "search:research:opaque", Workspace: "research", Source: "docs", Outcome: "succeeded"})
	h := NewSearchEvidenceHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search-evidence", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status=%d", res.Code)
	}
}
