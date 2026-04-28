package cli_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/metrofun/swobu/internal/adapters/inbound/cli"
)

func TestRunner_NoSubcommandFailsFastWithGuidanceInNonInteractiveMode(t *testing.T) {
	var stderr bytes.Buffer
	runner := cli.Runner{
		Stderr:        &stderr,
		IsInteractive: func() bool { return false },
	}

	exitCode := runner.Run(context.Background(), nil)
	if exitCode == cli.ExitHealthy {
		t.Fatalf("exit code = %d, want non-zero", exitCode)
	}
	if out := stderr.String(); !strings.Contains(out, "swobu status") {
		t.Fatalf("stderr = %q, want guidance to use swobu status", out)
	}
}

func TestRunner_NoSubcommandLaunchesInteractivePathWhenTerminalIsInteractive(t *testing.T) {
	var launched bool
	runner := cli.Runner{
		IsInteractive: func() bool { return true },
		AttachOrStart: func(context.Context, io.Writer, io.Writer, *http.Client) error { return nil },
		LaunchInteractive: func(context.Context, io.Reader, io.Writer, io.Writer) error {
			launched = true
			return nil
		},
	}

	exitCode := runner.Run(context.Background(), nil)
	if exitCode != cli.ExitHealthy {
		t.Fatalf("exit code = %d, want %d", exitCode, cli.ExitHealthy)
	}
	if !launched {
		t.Fatal("interactive launch path was not called")
	}
}
