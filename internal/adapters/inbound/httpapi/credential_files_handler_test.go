package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/credentialfiles"
)

func TestCredentialFilesHandlerReturnsMetadataOnly(t *testing.T) {
	handler := NewCredentialFilesHandler(func(_ context.Context, path string) (credentialfiles.Listing, error) {
		if path != `/home/operator/.config` {
			t.Fatalf("path = %q", path)
		}
		return credentialfiles.Listing{Path: path, Parent: "/home/operator", Entries: []credentialfiles.Entry{{Name: "key", Path: path + "/key"}}}, nil
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, `/_swobu/credential-files?path=%2Fhome%2Foperator%2F.config`, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{"contents", "permissions", "owner", "timestamp", "hash"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response exposes %q: %s", forbidden, body)
		}
	}
}

func TestCredentialFilesHandlerIsGETOnly(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewCredentialFilesHandler(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/_swobu/credential-files", nil))
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("status=%d allow=%q", recorder.Code, recorder.Header().Get("Allow"))
	}
}
