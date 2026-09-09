package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

func TestRunShareRefusesIncompatibleControlPlaneBeforeMutation(t *testing.T) {
	mutationCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/_swobu/status" {
			writer.Header().Set("Content-Type", "application/json")
			fmt.Fprint(writer, `{"state":"healthy","control_plane_protocol":8}`)
			return
		}
		mutationCalls++
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	t.Setenv(platformconfig.EnvAddr, strings.TrimPrefix(server.URL, "http://"))
	var stderr bytes.Buffer
	exitCode := runShare(context.Background(), server.Client(), &bytes.Buffer{}, &stderr, []string{"workspace/route"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want zero", mutationCalls)
	}
	if !strings.Contains(stderr.String(), "control-plane protocol 8") || !strings.Contains(stderr.String(), "requires 9") {
		t.Fatalf("stderr = %q, want protocol mismatch", stderr.String())
	}
}
