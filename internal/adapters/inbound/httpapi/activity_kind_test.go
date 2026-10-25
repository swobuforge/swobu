package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
)

func TestActivityKindFiltersWithoutFabricatingExchangesOrDroppingHistoryHealth(t *testing.T) {
	for _, kind := range []string{"exchange", "mcp_operation"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			handler := NewActivityHandler(func(request *http.Request, scope evidence.ProjectionScope, _ ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
				calls++
				return evidence.ActivityProjection{Scope: scope, Exchanges: []evidence.ActivityExchange{{ExchangeID: "exchange"}}, MCPOperations: []evidence.MCPOperation{{OperationID: "mcp"}}, History: &ledger.Health{State: ledger.StateDegraded, DroppedMCPOperationsTotal: 2}}, nil
			})
			writer := httptest.NewRecorder()
			handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity?kind="+kind, nil))
			var result evidence.ActivityProjection
			if writer.Code != http.StatusOK || json.Unmarshal(writer.Body.Bytes(), &result) != nil || result.History == nil || result.History.DroppedMCPOperationsTotal != 2 || calls != 1 {
				t.Fatal("kind filtering lost capture truth")
			}
			if (kind == "exchange" && (len(result.Exchanges) != 1 || len(result.MCPOperations) != 0)) || (kind == "mcp_operation" && (len(result.Exchanges) != 0 || result.Exchanges == nil || len(result.MCPOperations) != 1)) {
				t.Fatal("kind filter returned a different evidence class")
			}
		})
	}
}

func TestActivityAmbiguousOrUnsupportedQueryRejectsBeforeEvidenceRead(t *testing.T) {
	for _, query := range []string{"kind=unknown", "kind=", "kind=exchange&kind=mcp_operation", "workspace=research&workspace=other", "workspace=Research", "workspace=%20research", "workspace=research%20", "workspace=%zz", "limit=0", "limit=201", "limit=01", "limit=10&limit=20", "mcp_cursor=bad", "limit=1&mcp_cursor=bad", "admin=true"} {
		calls := 0
		handler := NewActivityHandler(func(*http.Request, evidence.ProjectionScope, ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
			calls++
			return evidence.ActivityProjection{}, nil
		})
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity?"+query, nil))
		if writer.Code != http.StatusBadRequest || calls != 0 {
			t.Fatal("ambiguous Activity query reached evidence owner", query)
		}
	}
}
