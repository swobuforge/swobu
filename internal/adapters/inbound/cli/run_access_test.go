package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAccessKeyAddAcceptsDocumentedPositionalThenFlagOrder(t *testing.T) {
	const accessID = "acc_AAAAAAAAAAAAAAAAAAAAAA"
	var commandPath, commandBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_swobu/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"state":"healthy","control_plane_protocol":10}`)
		case "/_swobu/access/" + accessID + "/credentials":
			commandPath = r.URL.Path
			body, _ := io.ReadAll(r.Body)
			commandBody = string(body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprint(w, `{"access":{"id":"`+accessID+`","name":"CI","kind":"managed"},"credential":{"id":"acred_AAAAAAAAAAAAAAAAAAAAAA","workspace":"prod"},"token":"swak_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SWOBU_ADDR", parsed.Host)
	var stdout, stderr strings.Builder
	code := runAccess(context.Background(), server.Client(), &stdout, &stderr, []string{"key", "add", accessID, "--workspace", "prod"})
	if code != ExitHealthy {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if commandPath == "" || !strings.Contains(commandBody, `"workspace":"prod"`) {
		t.Fatalf("command path/body = %q / %q", commandPath, commandBody)
	}
}
