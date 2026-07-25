package bootstrap_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/bootstrap"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

func TestDaemonLifecycle_CloseIsIdempotentAndEmitsStructuredLifecycleEvents(t *testing.T) {
	// Opt out so product telemetry does not attempt a real upload during the
	// test. (Not t.Parallel: t.Setenv.)
	t.Setenv("DO_NOT_TRACK", "1")

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "swobu.yaml")
	configYAML := "schema_version: 1\nworkspaces: {}\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{}))

	daemon, err := bootstrap.Start(context.Background(), bootstrap.StartInput{
		ConfigPath:    configPath,
		StartupConfig: platformconfig.StartupConfig{Addr: "127.0.0.1:0"},
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("bootstrap.Start returned error: %v", err)
	}
	if err := daemon.Close(); err != nil {
		t.Fatalf("daemon.Close returned error: %v", err)
	}
	if err := daemon.Close(); err != nil {
		t.Fatalf("second daemon.Close returned error: %v", err)
	}

	text := logs.String()
	assertContainsLogEvent(t, text, "intent_store_open_start")
	assertContainsLogEvent(t, text, "intent_store_open_success")
	assertContainsLogEvent(t, text, "bind_start")
	assertContainsLogEvent(t, text, "bind_success")
	assertContainsLogEvent(t, text, "initialization_completed")
	assertContainsLogEvent(t, text, "graceful_shutdown_requested")
	assertContainsLogEvent(t, text, "graceful_shutdown_completed")
	for _, event := range []string{"graceful_shutdown_requested", "graceful_shutdown_completed"} {
		if got := strings.Count(text, "event="+event); got != 1 {
			t.Fatalf("%s count = %d, want 1; logs=%s", event, got, text)
		}
	}
}

func TestDaemonLifecycle_StartFailureIncludesErrorDetailsInErrorAndLogs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	notDirectory := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(notDirectory, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	invalidConfigPath := filepath.Join(notDirectory, "swobu.yaml")
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{}))

	_, err := bootstrap.Start(context.Background(), bootstrap.StartInput{
		ConfigPath: invalidConfigPath,
		Logger:     logger,
	})
	if err == nil {
		t.Fatal("bootstrap.Start returned nil error for invalid config directory")
	}
	errText := err.Error()
	if !strings.Contains(errText, "not-a-directory") {
		t.Fatalf("error = %q, want invalid config path detail", errText)
	}
	if !strings.Contains(strings.ToLower(errText), "not a directory") { // swobu:io-string source=domain
		t.Fatalf("error = %q, want filesystem cause detail", errText)
	}

	logText := logs.String()
	assertContainsLogEvent(t, logText, "intent_store_open_failed")
	if !strings.Contains(logText, "config_path="+invalidConfigPath) {
		t.Fatalf("logs missing config_path detail; logs=%s", logText)
	}
	if !strings.Contains(strings.ToLower(logText), "not a directory") { // swobu:io-string source=domain
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
