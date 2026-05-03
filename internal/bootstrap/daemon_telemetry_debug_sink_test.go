package bootstrap_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/bootstrap"
)

func TestDaemonTelemetryDebug_SwapsSinkToStdout(t *testing.T) {
	t.Setenv("SWOBU_TELEMETRY_DEBUG", "1")
	t.Setenv("SWOBU_TELEMETRY_INTERVAL", "1")
	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	stateDoc := `{
  "enabled": true,
  "anonymous_install_id": "anon_debug",
  "first_seen_at": "2026-04-29T00:00:00Z",
  "notice_shown": true
}`
	if err := os.WriteFile(statePath, []byte(stateDoc), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("SWOBU_TELEMETRY_STATE_PATH", statePath)

	configPath := filepath.Join(t.TempDir(), "swobu.yaml")
	configYAML := "bind_addr: 127.0.0.1:0\nendpoints:\n  []\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe returned error: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	daemon, err := bootstrap.Start(context.Background(), bootstrap.StartInput{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("bootstrap.Start returned error: %v", err)
	}
	if err := daemon.Close(context.Background()); err != nil {
		t.Fatalf("daemon.Close returned error: %v", err)
	}
	_ = w.Close()

	var out bytes.Buffer
	if _, err := io.Copy(&out, r); err != nil {
		t.Fatalf("Copy returned error: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, `"telemetry_debug":true`) {
		t.Fatalf("stdout missing telemetry debug marker; stdout=%q", text)
	}
	if !strings.Contains(text, `"kind":"install"`) {
		t.Fatalf("stdout missing install event; stdout=%q", text)
	}
}
