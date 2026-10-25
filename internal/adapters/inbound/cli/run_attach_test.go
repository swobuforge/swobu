package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/producttelemetry"
)

func TestRunner_WebLaunchDoesNotStartBrowserWhenAttachFails(t *testing.T) {
	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")

	var launched bool
	var stderr bytes.Buffer
	runner := Runner{
		Stderr:        &stderr,
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error {
			return fmt.Errorf("daemon bootstrap failed")
		},
		OpenBrowser: func(string) error {
			launched = true
			return nil
		},
	}

	exitCode := runner.Run(context.Background(), nil)
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if launched {
		t.Fatal("browser was opened despite attach-or-start failure")
	}
}

func TestRunner_WebLaunchShowsNoticeBeforeAttachOrStart(t *testing.T) {
	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	attachCalled := false
	runner := Runner{
		Stdout:        &stdout,
		Stderr:        &stderr,
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error {
			attachCalled = true
			claimed, err := producttelemetry.ClaimNotice()
			if err != nil {
				t.Fatalf("ClaimNotice returned error: %v", err)
			}
			if claimed {
				t.Fatal("notice was not already claimed before attach/start")
			}
			return fmt.Errorf("stop after notice check")
		},
	}

	exitCode := runner.Run(context.Background(), nil)
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if !attachCalled {
		t.Fatal("attach/start was not called")
	}
	if stdout.String() == "" {
		t.Fatal("stdout is empty, want first-run notice")
	}
	out := stdout.String()
	if !strings.Contains(out, "privacy-minimized usage and reliability telemetry") {
		t.Fatalf("first-run telemetry notice missing before Web launch: %q", out)
	}
}

func TestDefaultAttachOrStart_AcceptsReachableDaemonWithoutPreviewProbe(t *testing.T) {
	var statusCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status" {
			t.Fatalf("unexpected path %q", r.URL.Path)
			return
		}
		statusCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"healthy","workspace_count":1,"control_plane_protocol":11}`)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	var stdout bytes.Buffer
	client := &http.Client{Timeout: 500 * time.Millisecond}
	err := defaultAttachOrStart(context.Background(), &stdout, io.Discard, client, addr, filepath.Join(t.TempDir(), "swobu.yaml"))
	if err != nil {
		t.Fatalf("defaultAttachOrStart returned error: %v", err)
	}
	if statusCalls == 0 {
		t.Fatal("status endpoint was not probed")
	}
}

func TestDefaultAttachOrStartRefusesIncompatibleControlPlane(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"healthy","control_plane_protocol":8}`)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	err := defaultAttachOrStart(context.Background(), io.Discard, io.Discard, &http.Client{}, addr, filepath.Join(t.TempDir(), "swobu.yaml"))
	if err == nil || !strings.Contains(err.Error(), "control-plane protocol 8") || !strings.Contains(err.Error(), "requires 11") {
		t.Fatalf("error = %v, want protocol mismatch diagnostic", err)
	}
}

func TestRunner_WebLaunchFlagsOverrideEnvironmentForAttachAndURL(t *testing.T) {
	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")
	t.Setenv(platformconfig.EnvDoNotTrack, "1")
	t.Setenv(platformconfig.EnvAddr, "127.0.0.1:8123")
	t.Setenv(platformconfig.EnvConfigPath, filepath.Join(t.TempDir(), "env-swobu.yaml"))
	const token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/browser-launches" || req.Method != http.MethodPost {
			t.Errorf("unexpected launch request: %s %s", req.Method, req.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"launch_path":"/open#`+token+`"}`)
	}))
	defer server.Close()
	flagAddr := strings.TrimPrefix(server.URL, "http://")
	flagConfig := filepath.Join(t.TempDir(), "flag-swobu.yaml")
	var attachAddr, attachConfig, opened string
	runner := Runner{
		HTTPClient: server.Client(),
		AttachOrStart: func(_ context.Context, _ io.Writer, _ io.Writer, _ *http.Client, addr, configPath string) error {
			attachAddr, attachConfig = addr, configPath
			return nil
		},
		OpenBrowser: func(target string) error { opened = target; return nil },
	}
	if code := runner.Run(t.Context(), []string{"open", "--addr", flagAddr, "--config", flagConfig}); code != ExitHealthy {
		t.Fatalf("open exit=%d", code)
	}
	if attachAddr != flagAddr || attachConfig != flagConfig || opened != server.URL+"/open#"+token {
		t.Fatalf("flags not respected: attach=%s config=%s opened=%s", attachAddr, attachConfig, opened)
	}
}
