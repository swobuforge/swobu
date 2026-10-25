package operatorclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNodeControlWorkspaceClientUsesVersionedRoot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workspaces" || r.Method != http.MethodGet {
			t.Fatalf("request = %s %s, want GET /api/v1/workspaces", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte("[]\n"))
	}))
	defer server.Close()
	if _, err := New(server.Client(), server.URL).ListWorkspaces(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNodeControlWorkspaceClientNeverFallsBackToLegacyRoot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_swobu/") {
			t.Fatalf("versioned client reached legacy control path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	_, _ = New(server.Client(), server.URL).ListWorkspaces(context.Background())
}

func TestNodeControlPrincipalListingUsesVersionedOntologyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/principals" || r.Method != http.MethodGet {
			t.Fatalf("request = %s %s, want GET /api/v1/principals", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte("[]\n"))
	}))
	defer server.Close()
	if _, err := New(server.Client(), server.URL).ListPrincipals(context.Background()); err != nil {
		t.Fatal(err)
	}
}
