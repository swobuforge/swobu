package operatorclient

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestPrincipalMetadataUsesCanonicalReadsWithoutFallback(t *testing.T) {
	for _, available := range []bool{true, false} {
		var paths []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path)
			if r.Method != http.MethodGet {
				t.Error("metadata inspection attempted a mutation")
			}
			if !available && (r.URL.Path == "/api/v1/principals/fixture" || r.URL.Path == "/api/v1/principals/fixture/credentials") {
				http.NotFound(w, r)
				return
			}
			if r.URL.Path == "/api/v1/principals/fixture/credentials" {
				_, _ = w.Write([]byte(`[{"id":"credential-fixture","workspace":"research"}]`))
			} else {
				_, _ = w.Write([]byte(`{"id":"fixture","label":"Fixture","status":"revoked"}`))
			}
		}))
		client := New(server.Client(), server.URL)
		principal, principalErr := client.GetPrincipal(t.Context(), "fixture")
		credentials, credentialErr := client.ListCredentials(t.Context(), "fixture")
		server.Close()
		if available {
			if principalErr != nil || credentialErr != nil || principal.ID != "fixture" || principal.Status != "revoked" || len(credentials) != 1 || credentials[0].Workspace != "research" {
				t.Fatal("canonical metadata projection failed")
			}
		} else if principalErr == nil || credentialErr == nil {
			t.Fatal("missing canonical metadata was masked by a healthy legacy response")
		}
		if !reflect.DeepEqual(paths, []string{"/api/v1/principals/fixture", "/api/v1/principals/fixture/credentials"}) {
			t.Fatal("metadata reads used noncanonical paths or retried")
		}
	}
}
