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
	"time"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

func TestRunner_InteractiveVersionNotice_ShowsInstallCommandBeforeAttach(t *testing.T) {
	originalFetch := fetchLatestVersion
	fetchLatestVersion = func() (string, error) { return "v999.0.0", nil }
	t.Cleanup(func() { fetchLatestVersion = originalFetch })

	t.Setenv(platformconfig.EnvSwobuHome, filepath.Join(t.TempDir(), "swobu-home"))
	t.Setenv(platformconfig.EnvDoNotTrack, "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	attachCalled := false
	runner := Runner{
		Stdout:        &stdout,
		Stderr:        &stderr,
		Stdin:         strings.NewReader("\n"),
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error {
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
	requireClosedNotice(t, text, "Update Available", []string{
		"versions: dev → v999.0.0",
		"update: " + installCommand,
		"hide: export " + platformconfig.EnvSkipVersionNotice + "=1",
	})
	if !strings.Contains(text, "press Enter to continue") {
		t.Fatalf("missing continue prompt; stdout=%q", text)
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

func TestDefaultFetchLatestVersionReadsLatestReleaseTag(t *testing.T) {
	originalClient := latestVersionHTTPClient
	latestVersionHTTPClient = &http.Client{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.String() != latestVersionURL {
				t.Fatalf("request URL = %q, want %q", request.URL.String(), latestVersionURL)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v9.8.7"}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	t.Cleanup(func() { latestVersionHTTPClient = originalClient })

	version, err := defaultFetchLatestVersion()
	if err != nil {
		t.Fatalf("defaultFetchLatestVersion() error = %v", err)
	}
	if version != "v9.8.7" {
		t.Fatalf("version = %q, want v9.8.7", version)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
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

func TestPatchOnlyVersionChange(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{name: "patch only", current: "v1.2.3", latest: "v1.2.9", want: true},
		{name: "major change", current: "v1.2.3", latest: "v2.0.0", want: false},
		{name: "minor change", current: "v1.2.3", latest: "v1.3.0", want: false},
		{name: "same version", current: "v1.2.3", latest: "v1.2.3", want: false},
		{name: "prerelease patch change", current: "v1.2.3-rc.1", latest: "v1.2.4", want: true},
		{name: "non semver current", current: "dev", latest: "v1.2.4", want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := patchOnlyVersionChange(tc.current, tc.latest)
			if got != tc.want {
				t.Fatalf("patchOnlyVersionChange(%q,%q)=%v, want %v", tc.current, tc.latest, got, tc.want)
			}
		})
	}
}

func TestRunner_InteractiveVersionNotice_FetchErrorDoesNotBlockAttach(t *testing.T) {
	originalFetch := fetchLatestVersion
	fetchLatestVersion = func() (string, error) { return "", errors.New("timeout") }
	t.Cleanup(func() { fetchLatestVersion = originalFetch })

	t.Setenv(platformconfig.EnvSwobuHome, filepath.Join(t.TempDir(), "swobu-home"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	attachCalled := false
	runner := Runner{
		Stdout:        &stdout,
		Stderr:        &stderr,
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error {
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
	if strings.Contains(stdout.String(), "╭─ Update Available ") {
		t.Fatalf("unexpected version notice on fetch error; stdout=%q", stdout.String())
	}
}

func TestRunner_InteractiveVersionNotice_MissingAcknowledgeInputContinuesToAttach(t *testing.T) {
	originalFetch := fetchLatestVersion
	fetchLatestVersion = func() (string, error) { return "v999.0.0", nil }
	t.Cleanup(func() { fetchLatestVersion = originalFetch })

	t.Setenv(platformconfig.EnvSwobuHome, filepath.Join(t.TempDir(), "swobu-home"))
	t.Setenv(platformconfig.EnvDoNotTrack, "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	attachCalled := false
	runner := Runner{
		Stdout:        &stdout,
		Stderr:        &stderr,
		Stdin:         strings.NewReader(""),
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error {
			attachCalled = true
			return nil
		},
		LaunchInteractive: func(context.Context, io.Reader, io.Writer, io.Writer, string) error { return nil },
		Sleep:             func(time.Duration) {},
	}

	exitCode := runner.Run(context.Background(), nil)
	if exitCode != ExitHealthy {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitHealthy)
	}
	if !attachCalled {
		t.Fatal("attach/start was not called")
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "press Enter to continue") {
		t.Fatalf("stdout missing continue prompt: %q", stdout.String())
	}
}
