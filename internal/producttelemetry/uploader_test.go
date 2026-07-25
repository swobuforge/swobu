package producttelemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReportUploader_PostsJSONAndAccepts2xx(t *testing.T) {
	var gotCT string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotCT = req.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(req.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	u := newReportUploader(srv.URL)
	report := productReport{Schema: productReportSchemaVersion, InstallID: "install-1"}
	if err := u.Upload(context.Background(), report); err != nil {
		t.Fatalf("upload returned error on 204: %v", err)
	}
	if gotCT != "application/json" {
		t.Fatalf("content-type = %q, want application/json", gotCT)
	}
	if !strings.Contains(string(gotBody), `"schema":1`) {
		t.Fatalf("body missing schema: %s", gotBody)
	}
}

func TestReportUploader_ReturnsErrorOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	u := newReportUploader(srv.URL)
	if err := u.Upload(context.Background(), productReport{Schema: productReportSchemaVersion}); err == nil {
		t.Fatal("upload to 400 should return an error")
	}
}

func TestReportUploader_RejectsOversizeBeforeRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	rows := make([]reportTrafficRow, 0, 5000)
	for range 5000 {
		rows = append(rows, reportTrafficRow{Count: 1})
	}
	u := newReportUploader(srv.URL)
	err := u.Upload(context.Background(), productReport{Schema: productReportSchemaVersion, Traffic: rows})
	if err == nil {
		t.Fatal("oversize report uploaded without error; 64 KiB ceiling not enforced")
	}
	if called {
		t.Fatal("oversize report must be rejected before any HTTP request is made")
	}
}
