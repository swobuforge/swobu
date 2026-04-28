package ptykit

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestStartCommandRejectsNil(t *testing.T) {
	t.Parallel()

	run, err := StartCommand(nil)
	if err == nil {
		t.Fatal("expected error for nil command")
	}
	if run != nil {
		t.Fatal("expected nil harness for nil command")
	}
}

func TestStartCommandWithSizeRejectsNil(t *testing.T) {
	t.Parallel()

	run, err := StartCommandWithSize(nil, 120, 40)
	if err == nil {
		t.Fatal("expected error for nil command")
	}
	if run != nil {
		t.Fatal("expected nil harness for nil command")
	}
}

func TestSendKeyRejectsUnknownName(t *testing.T) {
	t.Parallel()

	err := (&HarnessCloser{}).SendKey("Nope")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestHarnessCapturesOutputAndInput(t *testing.T) {
	cmd := exec.Command("sh", "-lc", "printf 'ready\\n'; IFS= read -r line; printf 'echo:%s\\n' \"$line\"")

	run, err := StartCommandWithSize(cmd, 140, 40)
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	defer func() {
		if closeErr := run.Close(); closeErr != nil {
			t.Fatalf("close run: %v", closeErr)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := run.WaitForContains(ctx, "ready"); err != nil {
		t.Fatalf("wait for ready: %v", err)
	}
	if err := run.SendRaw("hello from pty\r"); err != nil {
		t.Fatalf("send raw: %v", err)
	}
	if err := run.WaitForContains(ctx, "echo:hello from pty"); err != nil {
		t.Fatalf("wait for echoed input: %v", err)
	}
	if err := run.Wait(ctx); err != nil {
		t.Fatalf("wait for command exit: %v", err)
	}

	run.WaitCopyDone(250 * time.Millisecond)

	out := run.Output()
	if out == "" {
		t.Fatal("expected captured output")
	}
	if strings.TrimSpace(run.VisibleOutput()) == "" {
		t.Fatal("expected visible output")
	}
}

func TestVisibleOutputUsesTerminalEmulatorState(t *testing.T) {
	cmd := exec.Command("sh", "-lc", "printf 'abcdef\\rxy\\n'")

	run, err := StartCommandWithSize(cmd, 20, 3)
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	defer func() {
		if closeErr := run.Close(); closeErr != nil {
			t.Fatalf("close run: %v", closeErr)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := run.Wait(ctx); err != nil {
		t.Fatalf("wait for command exit: %v", err)
	}
	run.WaitCopyDone(250 * time.Millisecond)

	raw := run.Output()
	if !strings.Contains(raw, "abcdef") {
		t.Fatalf("raw output should contain original stream content: %q", raw)
	}

	visible := run.VisibleOutput()
	if !strings.Contains(visible, "xycdef") {
		t.Fatalf("visible output should reflect carriage-return rendering: %q", visible)
	}
}

func TestShutdownClosesAndWaits(t *testing.T) {
	cmd := exec.Command("sh", "-lc", "sleep 30")

	run, err := StartCommand(cmd)
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := run.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestWaitHonorsContext(t *testing.T) {
	t.Parallel()

	run := &HarnessCloser{waitCh: make(chan error)}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := run.Wait(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
