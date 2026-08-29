package logging

import (
	"bytes"
	"log"
	"log/slog"
	"strings"
	"testing"
)

func TestNewHandler_RedirectedOutputUsesStandardTextHandler(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	logger := slog.New(newHandler(&out, slog.LevelDebug, false))
	logger.Debug("debug probe", "component", "daemon")

	text := out.String()
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("redirected logs must not contain ANSI: %q", text)
	}
	for _, want := range []string{"level=DEBUG", "source=", "msg=\"debug probe\"", "component=daemon"} {
		if !strings.Contains(text, want) {
			t.Fatalf("redirected log missing %q: %q", want, text)
		}
	}
}

func TestNewHandler_InteractiveColorHonorsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var colored bytes.Buffer
	slog.New(newHandler(&colored, slog.LevelInfo, true)).Warn("probe")
	if !strings.Contains(colored.String(), "\x1b[") {
		t.Fatalf("interactive handler did not emit color: %q", colored.String())
	}

	t.Setenv("NO_COLOR", "1")
	var plain bytes.Buffer
	slog.New(newHandler(&plain, slog.LevelInfo, true)).Warn("probe")
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatalf("NO_COLOR handler emitted ANSI: %q", plain.String())
	}
}

func TestConfiguredLevel(t *testing.T) {
	t.Run("defaults to info", func(t *testing.T) {
		t.Setenv(envLogLevel, "")
		got, err := configuredLevel()
		if err != nil || got != slog.LevelInfo {
			t.Fatalf("configuredLevel() = %v, %v; want INFO, nil", got, err)
		}
	})
	t.Run("parses debug", func(t *testing.T) {
		t.Setenv(envLogLevel, "debug")
		got, err := configuredLevel()
		if err != nil || got != slog.LevelDebug {
			t.Fatalf("configuredLevel() = %v, %v; want DEBUG, nil", got, err)
		}
	})
	t.Run("rejects invalid", func(t *testing.T) {
		t.Setenv(envLogLevel, "debgu")
		if _, err := configuredLevel(); err == nil {
			t.Fatal("configuredLevel accepted invalid level")
		}
	})
}

func TestStandardLogUsesSlogDefaultWithoutFabricatedWarn(t *testing.T) {
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	log.Print("standard probe")

	text := out.String()
	if !strings.Contains(text, "msg=\"standard probe\"") {
		t.Fatalf("standard log did not reach slog default: %q", text)
	}
	if strings.Contains(text, "level=WARN") {
		t.Fatalf("standard log fabricated WARN severity: %q", text)
	}
}
