package bootstrap

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/platform/config"
)

// Operational controls are local-only and have one canonical identity. A
// Workspace bearer never grants Node authority, and obsolete endpoints never
// execute even when presented by a native local caller.
func TestDaemonCanonicalOperationalControlAndRetiredPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(t.Context(), StartInput{
		ConfigPath:    filepath.Join(root, "swobu.yaml"),
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	request := func(method, path string) *http.Response {
		t.Helper()
		r, err := http.NewRequestWithContext(t.Context(), method, daemon.BaseURL()+path, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, old := range []string{"/_swobu/down", "/_swobu/telemetry-report", "/_swobu/credential-files?path=%2F"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := request(method, old)
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("retired %s %s answered %d, want 404", method, old, response.StatusCode)
			}
		}
	}
	response := request(http.MethodGet, "/api/v1/telemetry/report")
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" ||
		!strings.Contains(response.Header.Get("Content-Type"), "application/json") || len(body) == 0 {
		t.Fatalf("canonical telemetry: status=%d headers=%v body=%s", response.StatusCode, response.Header, body)
	}
	response = request(http.MethodPost, "/api/v1/daemon/shutdown")
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("canonical shutdown: status=%d body=%s error=%v", response.StatusCode, body, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-daemon.serveDone:
	case <-ctx.Done():
		t.Fatal("canonical shutdown did not stop daemon")
	}
}
