package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestStartupTranscript_SplashAndDisclosurePrintOnce(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	tr := NewStartupTranscript(&out)
	tr.Emit(StartupEvent{Kind: StartupEventSplash})
	tr.Emit(StartupEvent{Kind: StartupEventSplash})
	tr.Emit(StartupEvent{Kind: StartupEventDisclosure})
	tr.Emit(StartupEvent{Kind: StartupEventDisclosure})

	text := out.String()
	if strings.Count(text, "|              SWOBU               |") != 1 {
		t.Fatalf("splash printed more than once; output=%q", text)
	}
	if strings.Count(text, "== startup disclosure ==") != 1 {
		t.Fatalf("disclosure printed more than once; output=%q", text)
	}
}

func TestStartupTranscript_EventRendering(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	tr := NewStartupTranscript(&out)
	tr.Emit(StartupEvent{Kind: StartupEventDaemonNotReachable, DaemonURL: "http://127.0.0.1:8080"})
	tr.Emit(StartupEvent{Kind: StartupEventDaemonRuntimeStart, ConfigPath: "/tmp/swobu.yaml"})
	tr.Emit(StartupEvent{Kind: StartupEventDaemonReady, State: "healthy"})

	text := out.String()
	assertContains(t, text, "daemon not reachable at http://127.0.0.1:8080")
	assertContains(t, text, "starting daemon runtime")
	assertContains(t, text, "config path: /tmp/swobu.yaml")
	assertContains(t, text, "daemon ready (healthy)")
}

func assertContains(t *testing.T, got string, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output missing %q; output=%q", want, got)
	}
}
