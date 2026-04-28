package bootstrap_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/metrofun/swobu/internal/bootstrap"
)

func TestDaemonLifecycle_EmitsStructuredLifecycleEvents(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "swobu.yaml")
	configYAML := "bind_addr: 127.0.0.1:0\nendpoints:\n  []\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{}))

	daemon, err := bootstrap.Start(context.Background(), bootstrap.StartInput{
		ConfigPath: configPath,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("bootstrap.Start returned error: %v", err)
	}
	if err := daemon.Close(context.Background()); err != nil {
		t.Fatalf("daemon.Close returned error: %v", err)
	}

	text := logs.String()
	assertContainsLogEvent(t, text, "intent_store_open_start")
	assertContainsLogEvent(t, text, "intent_store_open_success")
	assertContainsLogEvent(t, text, "bind_start")
	assertContainsLogEvent(t, text, "bind_success")
	assertContainsLogEvent(t, text, "initialization_completed")
	assertContainsLogEvent(t, text, "graceful_shutdown_requested")
	assertContainsLogEvent(t, text, "graceful_shutdown_completed")
}

func assertContainsLogEvent(t *testing.T, logs string, event string) {
	t.Helper()
	token := "event=" + event
	if !strings.Contains(logs, token) {
		t.Fatalf("logs missing %q; logs=%s", token, logs)
	}
}
