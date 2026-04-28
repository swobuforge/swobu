package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunner_InteractiveDoesNotLaunchCockpitWhenAttachOrStartFails(t *testing.T) {
	t.Parallel()

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
