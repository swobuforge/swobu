package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

func TestRunner_InteractiveVersionNotice_ShowsInstallCommandBeforeAttach(t *testing.T) {
	originalFetch := fetchLatestVersion
	fetchLatestVersion = func() (string, error) { return "v999.0.0", nil }
	t.Cleanup(func() { fetchLatestVersion = originalFetch })

	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	t.Setenv(platformconfig.EnvTelemetryStatePath, statePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	attachCalled := false
	runner := Runner{
		Stdout:        &stdout,
		Stderr:        &stderr,
		Stdin:         strings.NewReader("\n"),
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client) error {
			attachCalled = true
			return fmt.Errorf("stop after assertion")
		},
	}

	exitCode := runner.Run(context.Background(), nil)
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if !attachCalled {
		t.Fatal("attach/start was not called")
	}
	text := stdout.String()
	if !strings.Contains(text, "== version update notice ==") {
		t.Fatalf("missing version notice block; stdout=%q", text)
	}
	if !strings.Contains(text, installCommand) {
		t.Fatalf("missing install command; stdout=%q", text)
	}
	if !strings.Contains(text, "SWOBU_SKIP_VERSION_NOTICE") {
		t.Fatalf("missing skip env hint; stdout=%q", text)
	}
}

func TestEvaluateVersionNoticePolicy_ShowsOnMismatch(t *testing.T) {
	originalFetch := fetchLatestVersion
	fetchLatestVersion = func() (string, error) { return "v999.0.0", nil }
	t.Cleanup(func() { fetchLatestVersion = originalFetch })

	decision := evaluateVersionNoticePolicy()
	if !decision.show {
		t.Fatalf("show = false, want true")
	}
}

func TestEvaluateVersionNoticePolicy_SkipSuppressesNotice(t *testing.T) {
	originalFetch := fetchLatestVersion
	called := false
	fetchLatestVersion = func() (string, error) {
		called = true
		return "v999.0.0", nil
	}
	t.Cleanup(func() { fetchLatestVersion = originalFetch })

	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")

	decision := evaluateVersionNoticePolicy()
	if decision.show {
		t.Fatalf("show = true, want false when skip env set")
	}
	if called {
		t.Fatal("fetchLatestVersion called despite skip env")
	}
}

func TestEvaluateVersionNoticePolicy_NoNoticeOnMatch(t *testing.T) {
	originalFetch := fetchLatestVersion
	fetchLatestVersion = func() (string, error) { return "dev", nil }
	t.Cleanup(func() { fetchLatestVersion = originalFetch })

	decision := evaluateVersionNoticePolicy()
	if decision.show {
		t.Fatalf("show = true, want false on version match")
	}
}

func TestEvaluateVersionNoticePolicy_NoNoticeOnFetchError(t *testing.T) {
	originalFetch := fetchLatestVersion
	fetchLatestVersion = func() (string, error) { return "", errors.New("network down") }
	t.Cleanup(func() { fetchLatestVersion = originalFetch })

	decision := evaluateVersionNoticePolicy()
	if decision.show {
		t.Fatalf("show = true, want false on fetch error")
	}
}

func TestEvaluateVersionNoticePolicy_TrimsLatestVersionPayload(t *testing.T) {
	originalFetch := fetchLatestVersion
	fetchLatestVersion = func() (string, error) { return "\n  v999.0.0  \nextra-line\n", nil }
	t.Cleanup(func() { fetchLatestVersion = originalFetch })

	decision := evaluateVersionNoticePolicy()
	if !decision.show {
		t.Fatalf("show = false, want true with trimmed mismatched latest")
	}
	joined := strings.Join(decision.rows, "\n")
	if strings.Contains(joined, "extra-line") {
		t.Fatalf("notice contains unsanitized trailing payload; rows=%q", decision.rows)
	}
}

func TestRunner_InteractiveVersionNotice_FetchErrorDoesNotBlockAttach(t *testing.T) {
	originalFetch := fetchLatestVersion
	fetchLatestVersion = func() (string, error) { return "", errors.New("timeout") }
	t.Cleanup(func() { fetchLatestVersion = originalFetch })

	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	t.Setenv(platformconfig.EnvTelemetryStatePath, statePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	attachCalled := false
	runner := Runner{
		Stdout:        &stdout,
		Stderr:        &stderr,
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client) error {
			attachCalled = true
			return fmt.Errorf("stop after assertion")
		},
	}

	exitCode := runner.Run(context.Background(), nil)
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if !attachCalled {
		t.Fatal("attach/start was not called")
	}
	if strings.Contains(stdout.String(), "== version update notice ==") {
		t.Fatalf("unexpected version notice on fetch error; stdout=%q", stdout.String())
	}
}
