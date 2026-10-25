package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/app/operator/adminapproval"
)

func TestLimitsPreviewRejectsInvalidUnknownPricePolicyBeforeApproval(t *testing.T) {
	store, err := accounting.Open(t.TempDir() + "/accounting.json")
	if err != nil {
		t.Fatal(err)
	}
	handler := localAuthorityTestHandler(NewLimitsControlHandler(store, func(workspace string) bool { return workspace == "research" }).WithApprovals(adminapproval.New(time.Now)))
	revision := store.Revision()
	for _, policy := range []string{"", "allow", "observe_only", "Reject", "reject "} {
		limit := accounting.Limit{Currency: "USD", AmountCents: 1000, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: policy}
		payload, err := json.Marshal(limit)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/research/limits/preview", strings.NewReader(string(payload)))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || store.Revision() != revision {
			t.Fatalf("invalid policy %q preview status=%d", policy, response.Code)
		}
	}
}

func TestLimitsControlRejectsZeroThresholdBeforePublicationOrApproval(t *testing.T) {
	store, err := accounting.Open(t.TempDir() + "/accounting.json")
	if err != nil {
		t.Fatal(err)
	}
	handler := localAuthorityTestHandler(NewLimitsControlHandler(store, func(workspace string) bool { return workspace == "research" }).WithApprovals(adminapproval.New(time.Now)))
	revision := store.Revision()
	for _, mode := range []accounting.Mode{accounting.ModeStopNewBillableWork, accounting.ModeObserveOnly} {
		for _, operation := range []struct{ method, suffix string }{{http.MethodPut, ""}, {http.MethodPost, "/preview"}} {
			payload, err := json.Marshal(accounting.Limit{Currency: "USD", AmountCents: 0, Window: "day", Mode: mode, UnknownPrice: "reject"})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(operation.method, "/api/v1/workspaces/research/limits"+operation.suffix, strings.NewReader(string(payload)))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || store.Revision() != revision || !strings.Contains(response.Body.String(), "revoke permission") {
				t.Fatalf("zero threshold %s %s status=%d body=%s", mode, operation.suffix, response.Code, response.Body)
			}
		}
	}
}

func TestLimitsControlHandlerUsesDurableRevisionedNodeControl(t *testing.T) {
	store, err := accounting.Open(t.TempDir() + "/accounting.json")
	if err != nil {
		t.Fatal(err)
	}
	handler := localAuthorityTestHandler(NewLimitsControlHandler(store, func(workspace string) bool { return workspace == "research" }))
	unknown := httptest.NewRecorder()
	handler.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/unknown/limits", nil))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown status=%d", unknown.Code)
	}
	put := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/research/limits", strings.NewReader(`{"currency":"USD","amount_cents":1000,"window":"day","mode":"stop_new_billable_work","unknown_price":"reject"}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(put, req)
	if put.Code != http.StatusOK || put.Header().Get("ETag") == "" {
		t.Fatalf("put status/etag=%d/%q", put.Code, put.Header().Get("ETag"))
	}
	stale := httptest.NewRecorder()
	staleReq := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/research/limits", strings.NewReader(`{"currency":"USD","amount_cents":900,"window":"day","mode":"stop_new_billable_work","unknown_price":"reject"}`))
	staleReq.Header.Set("Content-Type", "application/json")
	staleReq.Header.Set("If-Match", `"stale"`)
	handler.ServeHTTP(stale, staleReq)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale status=%d", stale.Code)
	}
}

func TestLimitsApprovalPreviewApplyBindsDigestAndRevision(t *testing.T) {
	store, err := accounting.Open(t.TempDir() + "/accounting.json")
	if err != nil {
		t.Fatal(err)
	}
	approvals := adminapproval.New(nil)
	handler := localAuthorityTestHandler(NewLimitsControlHandler(store, func(string) bool { return true }).WithApprovals(approvals))
	payload := `{"currency":"USD","amount_cents":1000,"window":"day","mode":"stop_new_billable_work","unknown_price":"reject"}`
	preview := httptest.NewRecorder()
	previewReq := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/research/limits/preview", strings.NewReader(payload))
	previewReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(preview, previewReq)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	var approval adminapproval.Approval
	if err := json.Unmarshal(preview.Body.Bytes(), &approval); err != nil {
		t.Fatal(err)
	}
	apply := httptest.NewRecorder()
	body := `{"approval_id":"` + approval.ID + `","limit":` + payload + `}`
	applyReq := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/research/limits/apply", strings.NewReader(body))
	applyReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(apply, applyReq)
	if apply.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", apply.Code, apply.Body.String())
	}
	replay := httptest.NewRecorder()
	replayReq := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/research/limits/apply", strings.NewReader(body))
	replayReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(replay, replayReq)
	if replay.Code != http.StatusConflict {
		t.Fatalf("replay status=%d", replay.Code)
	}
}

func TestLimitsApprovalRejectsPromptShapedApprovalAndStaleRevision(t *testing.T) {
	store, err := accounting.Open(t.TempDir() + "/accounting.json")
	if err != nil {
		t.Fatal(err)
	}
	approvals := adminapproval.New(nil)
	handler := localAuthorityTestHandler(NewLimitsControlHandler(store, func(string) bool { return true }).WithApprovals(approvals))
	initial := `{"currency":"USD","amount_cents":1000,"window":"day","mode":"stop_new_billable_work","unknown_price":"reject"}`
	preview := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/research/limits/preview", strings.NewReader(initial))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(preview, request)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status=%d", preview.Code)
	}
	var approval adminapproval.Approval
	if err := json.Unmarshal(preview.Body.Bytes(), &approval); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLimit("research", accounting.Limit{Currency: "USD", AmountCents: 2000, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	changed := `{"currency":"USD","amount_cents":3000,"window":"day","mode":"stop_new_billable_work","unknown_price":"reject","approval":"approved"}`
	apply := httptest.NewRecorder()
	applyRequest := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/research/limits/apply", strings.NewReader(`{"approval_id":"`+approval.ID+`","limit":`+changed+`}`))
	applyRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(apply, applyRequest)
	if apply.Code != http.StatusConflict {
		t.Fatalf("stale/prompt-shaped apply status=%d body=%s", apply.Code, apply.Body.String())
	}
	limit, ok := store.Limit("research")
	if !ok || limit.AmountCents != 2000 {
		t.Fatalf("stale apply changed durable limit: %#v ok=%v", limit, ok)
	}
}

func TestLimitsControlHandlerAcceptsMonthlyWindow(t *testing.T) {
	store, err := accounting.Open(t.TempDir() + "/accounting.json")
	if err != nil {
		t.Fatal(err)
	}
	handler := localAuthorityTestHandler(NewLimitsControlHandler(store, func(workspace string) bool { return workspace == "research" }))
	request := httptest.NewRequest(http.MethodPut, "/api/v1/workspaces/research/limits", strings.NewReader(`{"currency":"USD","amount_cents":1000,"window":"month","mode":"stop_new_billable_work","unknown_price":"reject"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("monthly limit status=%d body=%s", response.Code, response.Body.String())
	}
	limit, ok := store.Limit("research")
	if !ok || limit.Window != "month" {
		t.Fatalf("monthly limit = %#v ok=%v", limit, ok)
	}
}
