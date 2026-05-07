package cli

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/bootstrap"
	"github.com/swobuforge/swobu/internal/telemetry"
)

func TestRunner_DaemonShowsNoticeBeforeStart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	t.Setenv("SWOBU_TELEMETRY_STATE_PATH", statePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	startCalled := false
	runner := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Start: func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error) {
			startCalled = true
			state, err := telemetry.NewStore().LoadOrCreate()
			if err != nil {
				t.Fatalf("LoadOrCreate returned error: %v", err)
			}
			if !state.NoticeShown {
				t.Fatal("notice_shown = false before daemon start")
			}
			return nil, fmt.Errorf("stop after notice check")
		},
	}

	exitCode := runner.Run(context.Background(), []string{"daemon", "--config", "/tmp/swobu-config.yaml"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if !startCalled {
		t.Fatal("daemon start was not called")
	}
	if stdout.String() == "" {
		t.Fatal("stdout is empty, want first-run notice")
	}
	out := stdout.String()
	if splash := strings.Index(out, "___.          "); splash < 0 {
		t.Fatalf("stdout missing splash; stdout=%q", out)
	} else if notice := strings.Index(out, "╭─ telemetry disclosure "); notice >= 0 && splash > notice {
		t.Fatalf("splash must render before telemetry disclosure; stdout=%q", out)
	}
}
