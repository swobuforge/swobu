package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestStartupTranscript_SplashPrintOnce(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	tr := NewStartupConsolePresenter(&out)
	tr.Emit(StartupEvent{Kind: StartupEventSplash})
	tr.Emit(StartupEvent{Kind: StartupEventSplash})

	text := out.String()
	if strings.Count(text, "___.          ") != 1 {
		t.Fatalf("splash printed more than once; output=%q", text)
	}
}

func TestStartupTranscript_EventRendering(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	tr := NewStartupConsolePresenter(&out)
	tr.Emit(StartupEvent{Kind: StartupEventDaemonNotReachable, DaemonURL: "http://127.0.0.1:8080"})
	tr.Emit(StartupEvent{Kind: StartupEventVersionNotice, Text: "current version: v0.1.0\nupdate now: curl -fsSL https://swobu.com/install.sh | sh"})
	tr.Emit(StartupEvent{Kind: StartupEventDaemonRuntimeStart, ConfigPath: "/tmp/swobu.yaml"})
	tr.Emit(StartupEvent{Kind: StartupEventDaemonReady, State: "healthy"})

	text := out.String()
	assertContains(t, text, "╭─ Update Available ")
	assertContains(t, text, "update now: curl -fsSL https://swobu.com/install.sh | sh")
	assertContains(t, text, "starting daemon runtime")
	assertContains(t, text, "config path: /tmp/swobu.yaml")
}

func assertContains(t *testing.T, got string, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output missing %q; output=%q", want, got)
	}
}
