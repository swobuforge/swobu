package runtime_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeTelemetry_OTLPExport_EmitsAggregateMetricsWithoutContentFields(t *testing.T) {
	t.Setenv("SWOBU_TELEMETRY_INTERVAL", "1")

	root := t.TempDir()
	statePath := filepath.Join(root, "telemetry", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	stateDoc := `{
  "enabled": true,
  "anonymous_install_id": "anon_test",
  "first_seen_at": "2026-04-29T00:00:00Z",
  "notice_shown": true
}`
	if err := os.WriteFile(statePath, []byte(stateDoc), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("SWOBU_TELEMETRY_STATE_PATH", statePath)

	reqCh := make(chan []byte, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/v1/metrics" {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		select {
		case reqCh <- body:
		default:
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	t.Setenv("SWOBU_TELEMETRY_ENDPOINT", server.URL)

	daemon := startDaemon(t, runtimeFixture{})
	time.Sleep(250 * time.Millisecond)
	if err := daemon.Close(context.Background()); err != nil {
		t.Fatalf("daemon.Close returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var body []byte
	for {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for OTLP telemetry export request")
		case payload := <-reqCh:
			if len(payload) == 0 {
				continue
			}
			body = payload
			goto gotPayload
		}
	}

gotPayload:
	if !strings.Contains(string(body), "swobu_installs_total") {
		t.Fatalf("otlp payload missing swobu_installs_total metric name; payload=%q", string(body))
	}
	lower := strings.ToLower(string(body))
	for _, forbidden := range []string{"messages", "prompt", "content", "tool_call", "authorization", "api_key"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("telemetry payload contains forbidden token %q; payload=%s", forbidden, string(body))
		}
	}
}
