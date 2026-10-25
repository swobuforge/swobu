package daemonlifecycle

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchStatusUsesCanonicalRouteWithoutLegacyFallback(t *testing.T) {
	for _, available := range []bool{true, false} {
		var paths []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path)
			if r.URL.Path == "/api/v1/status" && !available {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"healthy","control_plane_protocol":11}`))
		}))
		payload, class := FetchStatus(t.Context(), server.Client(), strings.TrimPrefix(server.URL, "http://"))
		server.Close()
		if len(paths) != 1 || paths[0] != "/api/v1/status" {
			t.Fatal("status used legacy route or fallback")
		}
		if available && (class == StatusClassDown || payload.ControlPlaneProtocol == nil || *payload.ControlPlaneProtocol != 11) {
			t.Fatal("canonical handshake not read")
		}
		if !available && class != StatusClassDown {
			t.Fatal("legacy success hid unavailable canonical status")
		}
	}
}
