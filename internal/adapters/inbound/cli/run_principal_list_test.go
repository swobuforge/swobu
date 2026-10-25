package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPrincipalListUsesCanonicalPrincipalProjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status":
			fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":11}`)
		case "/api/v1/principals":
			if r.Method != http.MethodGet {
				t.Fatalf("Principal list method=%s", r.Method)
			}
			fmt.Fprint(w, `[{"id":"acc_1","label":"Research app","kind":"managed","status":"active","permissions":[{"workspace":"research","action":"use"}]}]`)
		default:
			t.Fatalf("unexpected Principal list path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	address, _ := url.Parse(server.URL)
	var output, notices strings.Builder
	code := runPrincipal(t.Context(), server.Client(), &output, &notices, []string{"list"}, Runner{Addr: address.Host})
	if code != ExitHealthy || !strings.Contains(output.String(), "ID\tLABEL\tKIND\tSTATUS\tWORKSPACES") || !strings.Contains(output.String(), "acc_1\tResearch app\tmanaged\tactive\tresearch") {
		t.Fatalf("canonical Principal list failed: code=%d output=%q notices=%q", code, output.String(), notices.String())
	}
	if strings.Contains(output.String(), "local\t") {
		t.Fatalf("Principal listing invented a Local Principal: %q", output.String())
	}
}
