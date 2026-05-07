package logging

import (
	"bytes"
	"log"
	"log/slog"
	"regexp"
	"strings"
	"testing"
)

func TestCommonLineHandler_Format(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	logger := slog.New(NewCommonLineHandler(&out, slog.LevelInfo))
	logger.Info("daemon lifecycle",
		"component", "daemon",
		"event", "process_start",
		"config_path", "/tmp/swobu.yaml",
	)

	line := strings.TrimSpace(out.String())
	if line == "" {
		t.Fatal("expected one log line")
	}
	pattern := `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z INFO\s+\S+:\d+ component=daemon event=process_start config_path=/tmp/swobu\.yaml$`
	if !regexp.MustCompile(pattern).MatchString(line) {
		t.Fatalf("line does not match canonical format:\n%s", line)
	}
}

func TestConfigureDefaultLogger_RedirectsStdlibLog(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ConfigureDefaultLogger(&out)
	log.Printf("2026/05/06 09:56:42 failed to upload metrics: dial tcp timeout")

	text := strings.TrimSpace(out.String())
	if text == "" {
		t.Fatal("expected redirected stdlib log line")
	}
	if !strings.Contains(text, "WARN ") {
		t.Fatalf("expected WARN level for redirected stdlib log; got=%q", text)
	}
	if !strings.Contains(text, "component=stdlib") {
		t.Fatalf("expected stdlib component marker; got=%q", text)
	}
	if !strings.Contains(text, "failed to upload metrics: dial tcp timeout") {
		t.Fatalf("expected stdlib payload text; got=%q", text)
	}
}

func TestConfigureDefaultLogger_EnablesDebugWithEnv(t *testing.T) {
	t.Setenv("SWOBU_LOG_LEVEL", "debug")
	var out bytes.Buffer
	ConfigureDefaultLogger(&out)
	slog.Debug("debug probe", "component", "daemon")

	text := strings.TrimSpace(out.String())
	if !strings.Contains(text, "DEBUG") || !strings.Contains(text, "debug probe") {
		t.Fatalf("expected debug log line, got=%q", text)
	}
}
