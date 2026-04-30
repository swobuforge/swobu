package runtime_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/bootstrap"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
)

func TestRuntimeBootstrap_TelemetryEndpointDown_DoesNotBreakDaemonOrRequestPath(t *testing.T) {
	t.Setenv("SWOBU_TELEMETRY_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("SWOBU_TELEMETRY_INTERVAL", "1")

	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	stateDoc := `{
  "enabled": true,
  "anonymous_install_id": "anon_down",
  "first_seen_at": "2026-04-29T00:00:00Z",
  "notice_shown": true
}`
	if err := os.WriteFile(statePath, []byte(stateDoc), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("SWOBU_TELEMETRY_STATE_PATH", statePath)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	daemon := startDaemon(t, runtimeFixture{
		endpoints: []endpointintent.Endpoint{
			testEndpoint(t, "alpha", "backend-a", testProviderConfig(t, "backend-a", "custom", upstream.URL+"/v1", "", protocolsurface.ChatCompletions)),
		},
	})
	defer func() { _ = daemon.Close(context.Background()) }()

	time.Sleep(1200 * time.Millisecond)

	status, err := daemon.Status()
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if status.State != bootstrap.HealthStateHealthy {
		t.Fatalf("state = %q, want %q", status.State, bootstrap.HealthStateHealthy)
	}

	req, err := http.NewRequest(http.MethodPost, daemon.BaseURL()+"/c/alpha/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", resp.StatusCode, string(body))
	}
}
