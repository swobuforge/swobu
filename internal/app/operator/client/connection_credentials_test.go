package operatorclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReplaceConnectionCredentialUsesCanonicalRevisionBoundRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/connections/cn_search/credential-replacements" || r.Header.Get("If-Match") != `"7"` || r.Header.Get("Idempotency-Key") != "replace-1" {
			t.Fatalf("request method=%s path=%s headers=%v", r.Method, r.URL.Path, r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"same_identity":true`) || !strings.Contains(string(body), `"secret":"new-token"`) {
			t.Fatalf("request body=%s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connection":{"id":"cn_search","revision":"8"},"replayed":false}`))
	}))
	defer server.Close()
	result, err := New(server.Client(), server.URL).ReplaceConnectionCredential(context.Background(), "cn_search", "7", "replace-1", "new-token", "")
	if err != nil || result.Replayed || string(result.Connection["id"]) != `"cn_search"` {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
