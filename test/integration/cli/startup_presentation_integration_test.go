package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/adapters/inbound/cli"
	uicli "github.com/metrofun/swobu/internal/terminalui/apps/cli"
)

func TestRunner_InteractiveStartup_HandoffRenderedAfterFloor(t *testing.T) {
	var out bytes.Buffer
	var slept bool
	runner := cli.Runner{
		Stdout:        &out,
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client) error {
			return nil
		},
		Sleep: func(time.Duration) {
			slept = true
		},
		LaunchInteractive: func(context.Context, io.Reader, io.Writer, io.Writer) error {
			if !slept {
				t.Fatal("launch called before floor elapsed")
			}
			return nil
		},
	}

	exitCode := runner.Run(context.Background(), nil)
	if exitCode != cli.ExitHealthy {
		t.Fatalf("exit code = %d, want %d", exitCode, cli.ExitHealthy)
	}
	if !strings.Contains(out.String(), "[HANDOFF] entering interactive cockpit") {
		t.Fatalf("missing handoff line; out=%q", out.String())
	}
}

func TestRunner_InteractiveStartup_FailureSurfaceIncludesTerminalBlock(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	runner := cli.Runner{
		Stdout:        &out,
		Stderr:        &errOut,
		IsInteractive: func() bool { return true },
		AttachOrStart: func(_ context.Context, stdout io.Writer, _ io.Writer, _ *http.Client) error {
			tr := uicli.NewStartupTranscript(stdout)
			tr.Emit(uicli.StartupEvent{
				Kind: uicli.StartupEventStartupFailed,
				Text: "daemon readiness failed",
				NextAction: []string{
					"run `swobu status`",
				},
			})
			return errors.New("daemon readiness failed")
		},
	}

	exitCode := runner.Run(context.Background(), nil)
	if exitCode != cli.ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, cli.ExitDown)
	}
	if !strings.Contains(out.String(), "== startup failed ==") {
		t.Fatalf("missing startup failed block; out=%q", out.String())
	}
	if !strings.Contains(out.String(), "next: run `swobu status`") {
		t.Fatalf("missing next action in startup failed block; out=%q", out.String())
	}
	if !strings.Contains(errOut.String(), "daemon readiness failed") {
		t.Fatalf("stderr missing propagated error; stderr=%q", errOut.String())
	}
}
