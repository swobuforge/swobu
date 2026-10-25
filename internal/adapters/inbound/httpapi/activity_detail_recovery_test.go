package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/execution"
)

func TestActivityDetailServesSeparatedDeliveryAndUpstreamFactsWithoutProse(t *testing.T) {
	exchange, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	detail := evidence.OperationDetail{Kind: "exchange", Evidence: "ledger", Exchange: &evidence.ExchangeDetail{
		Identity: exchange.String(), Access: "local", Workspace: "research", Result: "client_write_failed",
		ErrorCode: "INTERNAL_ERROR", Upstream: evidence.UpstreamExecution{Recorded: 2, Occurred: 1, MayHaveOccurred: 1},
		Invocations: []evidence.InvocationDetail{
			{Identity: exchange.String() + "_1", Kind: "model", Route: "coding", ConnectionID: "cn_model", ConnectionRevision: 4, Occurrence: execution.Occurred},
			{Identity: exchange.String() + "_2", Kind: "model", Route: "coding", ConnectionID: "cn_model", ConnectionRevision: 4, Occurrence: execution.MayHaveOccurred},
		},
	}}
	calls := 0
	handler := ActivityDetailHandler{Read: func(_ *http.Request, requested string) (evidence.OperationDetail, bool, error) {
		calls++
		if requested != exchange.String() {
			t.Fatal("wrong history identity")
		}
		return detail, true, nil
	}}
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/activity/operations/"+exchange.String(), nil))
	if writer.Code != http.StatusOK || calls != 1 {
		t.Fatalf("detail status=%d calls=%d", writer.Code, calls)
	}
	var response map[string]any
	if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	row, ok := response["exchange"].(map[string]any)
	if !ok || row["error_code"] != "INTERNAL_ERROR" || row["delivery_result"] != "client_write_failed" {
		t.Fatal("client delivery and source truth conflated", writer.Body.String())
	}
	upstream, ok := row["upstream_execution"].(map[string]any)
	if !ok || upstream["occurred"] != float64(1) || upstream["may_have_occurred"] != float64(1) {
		t.Fatal("uncertain source result was lost")
	}
	if strings.Contains(writer.Body.String(), "Bearer ") || strings.Contains(writer.Body.String(), "prompt") || strings.Contains(writer.Body.String(), "tool_arguments") {
		t.Fatal("Activity detail leaked content or credentials")
	}
}
