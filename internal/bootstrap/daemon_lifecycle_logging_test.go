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

func TestDaemonLifecycle_StartFailureIncludesErrorDetailsInErrorAndLogs(t *testing.T) {
	t.Parallel()

	missingConfigPath := filepath.Join(t.TempDir(), "missing-swobu.yaml")
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{}))

	_, err := bootstrap.Start(context.Background(), bootstrap.StartInput{
		ConfigPath: missingConfigPath,
		Logger:     logger,
	})
	if err == nil {
		t.Fatal("bootstrap.Start returned nil error for missing config")
	}
	errText := err.Error()
	if !strings.Contains(errText, "missing-swobu.yaml") {
		t.Fatalf("error = %q, want missing config path detail", errText)
	}
	if !strings.Contains(strings.ToLower(errText), "no such file") {
		t.Fatalf("error = %q, want filesystem cause detail", errText)
	}

	logText := logs.String()
	assertContainsLogEvent(t, logText, "intent_store_open_failed")
	if !strings.Contains(logText, "config_path="+missingConfigPath) {
		t.Fatalf("logs missing config_path detail; logs=%s", logText)
	}
	if !strings.Contains(strings.ToLower(logText), "no such file") {
		t.Fatalf("logs missing underlying error detail; logs=%s", logText)
	}
}

func assertContainsLogEvent(t *testing.T, logs string, event string) {
	t.Helper()
	token := "event=" + event
	if !strings.Contains(logs, token) {
		t.Fatalf("logs missing %q; logs=%s", token, logs)
	}
}
