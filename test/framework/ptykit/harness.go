package ptykit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

var namedKeys = map[string]string{
	"Enter":     "\r",
	"Esc":       "\x1b",
	"Backspace": "\x7f",
	"Up":        "\x1b[A",
	"Down":      "\x1b[B",
	"Right":     "\x1b[C",
	"Left":      "\x1b[D",
	"Tab":       "\t",
	"ShiftTab":  "\x1b[Z",
	"CtrlC":     "\x03",
}

type lockedWriter struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedWriter) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// HarnessCloser wraps a running PTY-backed command for tests.
type HarnessCloser struct {
	cmd      *exec.Cmd
	ptmx     *os.File
	out      *lockedWriter
	view     vt10x.Terminal
	waitCh   chan error
	copyDone chan struct{}
}

func StartCommand(cmd *exec.Cmd) (*HarnessCloser, error) {
	if cmd == nil {
		return nil, fmt.Errorf("nil command")
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	return startWithPTY(cmd, ptmx, 80, 24), nil
}

func StartCommandWithSize(cmd *exec.Cmd, cols int, rows int) (*HarnessCloser, error) {
	if cmd == nil {
		return nil, fmt.Errorf("nil command")
	}
	if cols <= 0 {
		cols = 110
	}
	if rows <= 0 {
		rows = 32
	}

	ws := &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, err
	}

	return startWithPTY(cmd, ptmx, cols, rows), nil
}

func startWithPTY(cmd *exec.Cmd, ptmx *os.File, cols int, rows int) *HarnessCloser {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	run := &HarnessCloser{
		cmd:      cmd,
		ptmx:     ptmx,
		out:      &lockedWriter{},
		view:     vt10x.New(vt10x.WithSize(cols, rows)),
		waitCh:   make(chan error, 1),
		copyDone: make(chan struct{}),
	}

	go func() {
		_, _ = io.Copy(io.MultiWriter(run.out, run.view), run.ptmx)
		close(run.copyDone)
	}()

	go func() {
		run.waitCh <- cmd.Wait()
	}()

	return run
}

func (h *HarnessCloser) SendRaw(raw string) error {
	if h == nil || h.ptmx == nil {
		return fmt.Errorf("pty harness not started")
	}

	_, err := io.WriteString(h.ptmx, raw)
	return err
}

func (h *HarnessCloser) SendKey(name string) error {
	raw, ok := namedKeys[name]
	if !ok {
		return fmt.Errorf("unknown key %q", name)
	}

	return h.SendRaw(raw)
}

func (h *HarnessCloser) WaitForContains(ctx context.Context, needle string) error {
	if h == nil {
		return fmt.Errorf("nil harness")
	}

	needle = strings.TrimSpace(needle)
	if needle == "" {
		return nil
	}

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		if strings.Contains(h.Output(), needle) {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for %q: %w\noutput:\n%s", needle, ctx.Err(), h.Output())
		case <-ticker.C:
		}
	}
}

func (h *HarnessCloser) Wait(ctx context.Context) error {
	if h == nil {
		return nil
	}

	select {
	case err := <-h.waitCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *HarnessCloser) Output() string {
	if h == nil || h.out == nil {
		return ""
	}

	return h.out.String()
}

func (h *HarnessCloser) VisibleOutput() string {
	if h == nil || h.view == nil {
		return ""
	}
	return h.view.String()
}

func (h *HarnessCloser) Close() error {
	if h == nil || h.ptmx == nil {
		return nil
	}

	return h.ptmx.Close()
}

func (h *HarnessCloser) Shutdown(ctx context.Context) error {
	if h == nil {
		return nil
	}

	closeErr := h.Close()
	waitErr := h.Wait(ctx)
	h.WaitCopyDone(time.Second)

	switch {
	case closeErr == nil:
		if waitErr == nil {
			return nil
		}
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return nil
		}
		return waitErr
	case waitErr == nil:
		return closeErr
	case errors.Is(waitErr, os.ErrClosed):
		return closeErr
	default:
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return closeErr
		}
		return errors.Join(closeErr, waitErr)
	}
}

func (h *HarnessCloser) WaitCopyDone(timeout time.Duration) {
	if h == nil || h.copyDone == nil {
		return
	}
	if timeout <= 0 {
		timeout = time.Second
	}

	select {
	case <-h.copyDone:
	case <-time.After(timeout):
	}
}
