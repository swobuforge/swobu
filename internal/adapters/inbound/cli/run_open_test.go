package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

func TestOpenUsesOnlyNativeLaunchEndpointAndDoesNotLogCapability(t *testing.T) {
	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")
	t.Setenv(platformconfig.EnvDoNotTrack, "1")
	const token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls++
		if req.Method != http.MethodPost || req.URL.Path != "/api/v1/browser-launches" || req.URL.RawQuery != "" || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
			t.Errorf("not a local native launch: %s %s headers=%v", req.Method, req.URL, req.Header)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"launch_path":"/open#`+token+`"}`)
	}))
	defer srv.Close()
	var stdout, stderr bytes.Buffer
	opened := ""
	runner := Runner{
		HTTPClient: srv.Client(), Stdout: &stdout, Stderr: &stderr,
		Addr:          strings.TrimPrefix(srv.URL, "http://"),
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error { return nil },
		OpenBrowser:   func(url string) error { opened = url; return nil },
	}
	if code := runner.Run(t.Context(), nil); code != ExitHealthy {
		t.Fatalf("default launch code=%d stderr=%s", code, stderr.String())
	}
	if opened != srv.URL+"/open#"+token || calls != 1 {
		t.Fatalf("opened=%q requests=%d", opened, calls)
	}
	if strings.Contains(stdout.String()+stderr.String(), token) {
		t.Fatalf("launch capability leaked to stdout or stderr")
	}
	if !strings.Contains(stdout.String(), srv.URL+"/workspaces") {
		t.Fatalf("missing safe URL: %q", stdout.String())
	}
}

func TestOpenRejectsInvalidOrUntrustedLaunch(t *testing.T) {
	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")
	t.Setenv(platformconfig.EnvDoNotTrack, "1")
	for _, tc := range []struct {
		name     string
		response string
		status   int
	}{
		{"untrusted absolute origin", `{"launch_path":"https://evil.example/open#AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`, http.StatusCreated},
		{"replayed path", `{"launch_path":"/open#x"}`, http.StatusCreated},
		{"query injection", `{"launch_path":"/open#AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA?x=y"}`, http.StatusCreated},
		{"bad JSON", `?`, http.StatusCreated},
		{"refused by daemon", `{}`, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.response)
			}))
			defer srv.Close()
			var stderr bytes.Buffer
			opened := false
			runner := Runner{
				Addr:       strings.TrimPrefix(srv.URL, "http://"),
				HTTPClient: srv.Client(), Stderr: &stderr, Stdout: io.Discard,
				AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error { return nil },
				OpenBrowser:   func(string) error { opened = true; return nil },
			}
			if code := runner.Run(t.Context(), []string{"open"}); code != ExitDown || opened {
				t.Fatalf("unsafe browser open permitted: code=%d opened=%v", code, opened)
			}
		})
	}
}

func TestOpenBrowserFailureDoesNotExposeOneShotToken(t *testing.T) {
	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")
	t.Setenv(platformconfig.EnvDoNotTrack, "1")
	const token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"launch_path":"/open#`+token+`"}`)
	}))
	defer srv.Close()
	var stdout, stderr bytes.Buffer
	runner := Runner{
		Addr: strings.TrimPrefix(srv.URL, "http://"), HTTPClient: srv.Client(),
		Stdout: &stdout, Stderr: &stderr,
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error { return nil },
		OpenBrowser:   func(url string) error { return errors.New("no desktop opener: " + url) },
	}
	if code := runner.Run(t.Context(), []string{"open"}); code != ExitDown || !strings.Contains(stderr.String(), "could not open the local Web Operator") || strings.Contains(stdout.String()+stderr.String(), token) {
		t.Fatalf("browser failure was not safe: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestOpenRejectsNonLocalAddressBeforeAttach(t *testing.T) {
	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")
	called := false
	runner := Runner{AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error {
		called = true
		return nil
	}}
	if code := runner.Run(t.Context(), []string{"open", "--addr", "example.com:7926"}); code != ExitDown || called {
		t.Fatalf("nonlocal browser control address permitted: code=%d attach=%v", code, called)
	}
}

func TestOpenConfigFlagIsResolved(t *testing.T) {
	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")
	path := filepath.Join(t.TempDir(), "node.yaml")
	var got string
	runner := Runner{AttachOrStart: func(_ context.Context, _, _ io.Writer, _ *http.Client, _, configPath string) error {
		got = configPath
		return errors.New("stop after config check")
	}}
	runner.Run(t.Context(), []string{"open", "--config", path})
	if got != path {
		t.Fatalf("config=%s expected=%s", got, path)
	}
}
