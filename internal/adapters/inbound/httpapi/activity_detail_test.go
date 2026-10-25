package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
)

func TestActivityDetailRejectsMalformedIdentityBeforeRead(t *testing.T) {
	for _, suffix := range []string{"", "mcp_", "mcp_a/other", "mcp_%61", "mcp_a?workspace=coding", "ex_bad", "../status", "unknown"} {
		calls := 0
		handler := ActivityDetailHandler{Read: func(*http.Request, string) (evidence.OperationDetail, bool, error) {
			calls++
			return evidence.OperationDetail{}, false, nil
		}}
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity/operations/"+suffix, nil))
		if writer.Code != http.StatusBadRequest || calls != 0 {
			t.Fatal("malformed detail reached evidence", suffix)
		}
	}
}
