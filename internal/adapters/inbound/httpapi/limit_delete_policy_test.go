package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/accounting"
)

func TestLimitDeleteRequiresStrongETagAndKeepsExecutionEvidence(t *testing.T) {
	store, err := accounting.Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	limit := accounting.Limit{Currency: "USD", AmountCents: 150, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"}
	if err := store.SetLimit("research", limit); err != nil {
		t.Fatal(err)
	}
	reservation, err := store.Reserve("research", "exchange", 20, true)
	if err != nil {
		t.Fatal(err)
	}
	etag := `"` + store.Revision() + `"`
	handler := localAuthorityTestHandler(NewLimitsControlHandler(store, func(name string) bool { return name == "research" }))
	perform := func(match string) int {
		r := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/research/limits", nil)
		if match != "" {
			r.Header.Set("If-Match", match)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	if code := perform(""); code != http.StatusPreconditionRequired {
		t.Fatalf("missing precondition=%d", code)
	}
	if code := perform(`"stale"`); code != http.StatusPreconditionFailed {
		t.Fatalf("stale precondition=%d", code)
	}
	if code := perform(etag); code != http.StatusNoContent {
		t.Fatalf("clear result=%d", code)
	}
	if _, ok := store.Limit("research"); ok {
		t.Fatal("budget still configured")
	}
	if got := store.Reservations("research"); len(got) != 1 || got[0].Operation != reservation.Operation {
		t.Fatalf("reservation erased %#v", got)
	}
}
