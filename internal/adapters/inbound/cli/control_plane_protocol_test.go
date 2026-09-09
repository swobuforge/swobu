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

func TestRecoveryCommandsRemainAvailableAcrossProtocolMismatch(t *testing.T) {
	downCalls := 0
	statusCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/_swobu/status":
			statusCalls++
			writer.Header().Set("Content-Type", "application/json")
			if downCalls == 0 {
				fmt.Fprint(writer, `{"state":"healthy","control_plane_protocol":8}`)
			} else {
				writer.WriteHeader(http.StatusServiceUnavailable)
			}
		case "/_swobu/down":
			downCalls++
			writer.WriteHeader(http.StatusOK)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	t.Setenv(platformconfig.EnvAddr, addr)
	var statusOut bytes.Buffer
	if exitCode := runStatus(context.Background(), server.Client(), &statusOut, &bytes.Buffer{}, nil); exitCode != ExitHealthy {
		t.Fatalf("status exit code = %d, want healthy; output=%q", exitCode, statusOut.String())
	}
	if !strings.Contains(statusOut.String(), `"control_plane_protocol":8`) {
		t.Fatalf("status output = %q, want mismatch visible", statusOut.String())
	}
	if exitCode := runDown(context.Background(), server.Client(), &bytes.Buffer{}, &bytes.Buffer{}, []string{"--timeout", "1s"}); exitCode != ExitHealthy {
		t.Fatalf("down exit code = %d, want healthy", exitCode)
	}
	if downCalls != 1 || statusCalls < 2 {
		t.Fatalf("down calls=%d status calls=%d", downCalls, statusCalls)
	}
}
