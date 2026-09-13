package operatorclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/sharestate"
)

func TestShareClientPreservesRouteWireSpellingForWorkspaceRefs(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.Method {
		case http.MethodGet:
			if got := r.URL.Query().Get("route"); got != "dev" || r.URL.Query().Has("ref") {
				t.Fatalf("reveal query = %q, want route=dev only", r.URL.RawQuery)
			}
			writeShareResult(w)
		case http.MethodPost, http.MethodDelete:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["route"] != "dev" || body["ref"] != nil {
				t.Fatalf("%s body = %#v, want route=dev without ref", r.Method, body)
			}
			if r.Method == http.MethodPost {
				writeShareResult(w)
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
		}
	}))
	defer server.Close()

	client := New(server.Client(), server.URL)
	if _, err := client.RevealShare(context.Background(), "dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.IssueShare(context.Background(), "dev", sharestate.ExpiryOneDay); err != nil {
		t.Fatal(err)
	}
	if err := client.RevokeShare(context.Background(), "dev"); err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func writeShareResult(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"share_url":"https://d-example.share.swobu.com/#swsh_example"}`))
}
