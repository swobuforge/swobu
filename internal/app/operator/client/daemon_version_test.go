package operatorclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDaemonVersionUsesCanonicalStatusWithoutFallback(t *testing.T) {
	for _, available := range []bool{true, false} {
		var paths []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path)
			if r.URL.Path == "/api/v1/status" && !available {
				w.WriteHeader(404)
				return
			}
			_, _ = w.Write([]byte(`{"state":"healthy","swobu_version":"fixture-version"}`))
		}))
		version, err := New(server.Client(), server.URL).DaemonVersion(t.Context())
		server.Close()
		if len(paths) != 1 || paths[0] != "/api/v1/status" {
			t.Fatal("version used legacy status or fallback")
		}
		if available && (err != nil || version != "fixture-version") {
			t.Fatal("canonical version not returned")
		}
		if !available && err == nil {
			t.Fatal("legacy status hid unavailable canonical version")
		}
	}
}
