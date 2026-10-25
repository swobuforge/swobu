package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestLimitsStatusRequiresLocalAdmissionAndDisablesCaching(t *testing.T) {
	store, err := accounting.Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reserve("research", "attempt", 7, false); err != nil {
		t.Fatal(err)
	}
	handler := NewLimitsControlHandler(store, func(workspace string) bool { return workspace == "research" })
	revision := store.Revision()
	for _, local := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/research/limits/status", nil)
		if local {
			request = request.WithContext(context.WithValue(request.Context(), admissionContextKey{}, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.Local, nil })))
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if !local {
			if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "charged_cents") {
				t.Fatal("unadmitted status exposure")
			}
			continue
		}
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status response=%d cache=%q", response.Code, response.Header().Get("Cache-Control"))
		}
		var status accounting.Status
		if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		if status.Workspace != "research" || status.ChargedCents != "7" || status.UnknownPriceOperations != 1 || status.Limit != nil || status.RemainingCents != nil {
			t.Fatalf("status lost accounting truth=%#v", status)
		}
	}
	if store.Revision() != revision {
		t.Fatal("status requests mutated accounting")
	}
}

func TestLimitChangesRequireRealLocalAdmission(t *testing.T) {
	store, err := accounting.Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	limit := accounting.Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"}
	if err := store.ApplyReviewedLimitChecked(t.Context(), "research", limit, accounting.LimitChange{ApprovalID: "approval", Workspace: "research", Actor: "local", Digest: "digest", Revision: store.Revision()}, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	h := NewLimitsControlHandler(store, func(string) bool { return true })
	for _, local := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/research/limits/changes", nil)
		if local {
			req = req.WithContext(context.WithValue(req.Context(), admissionContextKey{}, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) { return accessidentity.Local, nil })))
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		if local {
			if out.Code != 200 || !strings.Contains(out.Body.String(), `"actor":"local"`) {
				t.Fatal("local audit readback missing")
			}
		} else if out.Code != 403 || strings.Contains(out.Body.String(), `"actor":"local"`) {
			t.Fatal("unadmitted audit exposure")
		}
	}
}
