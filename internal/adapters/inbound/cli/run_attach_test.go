package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/telemetry"
)

func TestRunner_InteractiveDoesNotLaunchCockpitWhenAttachOrStartFails(t *testing.T) {
	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")

	var launched bool
	var stderr bytes.Buffer
	runner := Runner{
		Stderr:        &stderr,
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client) error {
			return fmt.Errorf("daemon bootstrap failed")
		},
		LaunchInteractive: func(context.Context, io.Reader, io.Writer, io.Writer) error {
			launched = true
			return nil
		},
	}

	exitCode := runner.Run(context.Background(), nil)
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if launched {
		t.Fatal("launchInteractive was called despite attach-or-start failure")
	}
}

func TestRunner_InteractiveShowsNoticeBeforeAttachOrStart(t *testing.T) {
	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	attachCalled := false
	runner := Runner{
		Stdout:        &stdout,
		Stderr:        &stderr,
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client) error {
			attachCalled = true
			state, err := telemetry.NewStore().LoadOrCreate()
			if err != nil {
				t.Fatalf("LoadOrCreate returned error: %v", err)
			}
			if !state.NoticeShown {
				t.Fatal("notice_shown = false before attach/start")
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
}

func TestDefaultAttachOrStart_AcceptsReachableDaemonWithoutPreviewProbe(t *testing.T) {
	var statusCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/status" {
			t.Fatalf("unexpected path %q", r.URL.Path)
			return
		}
		statusCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"healthy","endpoint_count":1}`)
	}))
	defer srv.Close()

	t.Setenv("SWOBU_DAEMON_URL", srv.URL)
	var stdout bytes.Buffer
	client := &http.Client{Timeout: 500 * time.Millisecond}
	err := defaultAttachOrStart(context.Background(), &stdout, io.Discard, client)
	if err != nil {
		t.Fatalf("defaultAttachOrStart returned error: %v", err)
	}
	if statusCalls == 0 {
		t.Fatal("status endpoint was not probed")
	}
}

func TestRunner_InteractivePrintsHandoffEventBeforeLaunch(t *testing.T) {
	t.Setenv(platformconfig.EnvSkipVersionNotice, "1")
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))

	var stdout bytes.Buffer
	runner := Runner{
		Stdout:        &stdout,
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client) error { return nil },
		Sleep:         func(time.Duration) {},
		LaunchInteractive: func(context.Context, io.Reader, io.Writer, io.Writer) error {
			return nil
		},
	}

	exitCode := runner.Run(context.Background(), nil)
	if exitCode != ExitHealthy {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitHealthy)
	}
	if got := stdout.String(); bytes.Contains([]byte(got), []byte("entering interactive cockpit")) {
		t.Fatalf("stdout should not include slog phase log event; stdout=%q", got)
	}
}
