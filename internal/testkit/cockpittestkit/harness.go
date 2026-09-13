package testkit

import (
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	"github.com/swobuforge/swobu/internal/testkit/testscreen"
)

// MockAppHarness is a lightweight, in-process interactive test fixture for go-tui
// components. It uses a mock terminal and a seeded App instance so cockpit
// tests stay deterministic without depending on a real PTY.
//
// App construction delegates to mountedrender, which seeds framework internals.
// This harness does not prove the full upstream app loop.
//
// Use MockAppHarness for temporal tests that need focus management or event dispatch.
// For one-shot mounted component assertions, prefer RenderMountedString /
// RenderMountedScreen. Use RenderString / RenderScreen only for already-built
// inert element trees.
type MockAppHarness struct {
	app    *tui.App
	reader *tui.MockEventReader
}

// NewHarness creates an interactive test fixture for the given root component.
func NewHarness(root tui.Component) (*MockAppHarness, error) {
	return NewHarnessAt(root, 120, 40)
}

// NewHarnessAt creates an interactive fixture at an explicit viewport. Use it
// when one keyboard path must produce width-specific visual evidence.
func NewHarnessAt(root tui.Component, width, height int) (*MockAppHarness, error) {
	app, reader, err := mountedrender.NewApp(width, height)
	if err != nil {
		return nil, err
	}

	app.SetRootComponent(root)
	return &MockAppHarness{app: app, reader: reader}, nil
}

// FrameTrimmed renders and returns the current buffer without trailing cells.
func (h *MockAppHarness) FrameTrimmed() string {
	h.Frame()
	return h.app.Buffer().StringTrimmed()
}

// Screen renders and returns the styled terminal-cell state, failing the test
// when the buffer contains a cell outside the canonical fixture contract.
func (h *MockAppHarness) Screen(t testing.TB) testscreen.Screen {
	t.Helper()
	h.Frame()
	screen, err := ScreenFromBuffer(h.app.Buffer())
	if err != nil {
		t.Fatalf("capture harness screen: %v", err)
	}
	return screen
}

// NewFuncHarness creates a MockAppHarness from a bare element tree instead of a
// Component. It is useful when the production root (e.g., *Cockpit) renders via
// WorkspacePage surface components and you only need the outer element tree.
func NewFuncHarness(root *tui.Element) (*MockAppHarness, error) {
	app, reader, err := mountedrender.NewApp(120, 40)
	if err != nil {
		return nil, err
	}

	app.SetRoot(root)
	return &MockAppHarness{app: app, reader: reader}, nil
}

// App returns the underlying go-tui App.
func (h *MockAppHarness) App() *tui.App { return h.app }

// Reader returns the MockEventReader so callers can inject events.
func (h *MockAppHarness) Reader() *tui.MockEventReader { return h.reader }

// Frame renders the component tree and returns the current buffer contents as
// a string. Focus must be established before rendering (FocusNext / traversal).
func (h *MockAppHarness) Frame() string {
	h.app.Render()
	h.flushQueuedUpdates()
	return h.app.Buffer().String()
}

// DispatchKey sends a key event through the app's dispatch system and re-renders.
// Use this for deterministic keyboard tests with MockAppHarness.
func (h *MockAppHarness) DispatchKey(keyEvent tui.KeyEvent) {
	h.app.Dispatch(keyEvent)
	h.app.Render()
	h.flushQueuedUpdates()
}

// FocusNext focuses the next focusable element and renders the frame.
func (h *MockAppHarness) FocusNext() {
	h.app.FocusNext()
}

// FocusPrev focuses the previous focusable element and renders the frame.
func (h *MockAppHarness) FocusPrev() {
	h.app.FocusPrev()
}

// Open renders the app once so dispatch tables and initial focus state are ready.
func (h *MockAppHarness) Open() {
	h.app.MarkDirty()
	h.app.Render()
	h.flushQueuedUpdates()
}

// Close shuts down the harness.
// Do not use the harness after Close.
func (h *MockAppHarness) Close() {
	if h.app != nil {
		_ = h.app.Close()
	}
}

func (h *MockAppHarness) flushQueuedUpdates() {
	if h.app == nil {
		return
	}

	const settleWindow = 2 * time.Millisecond

	for {
		processed := false
		for {
			select {
			case ev := <-h.app.Events():
				processed = true
				h.app.Dispatch(ev)
				h.app.Render()
			default:
				goto settle
			}
		}

	settle:
		if processed {
			continue
		}

		timer := time.NewTimer(settleWindow)
		select {
		case ev := <-h.app.Events():
			if !timer.Stop() {
				<-timer.C
			}
			h.app.Dispatch(ev)
			h.app.Render()
		case <-timer.C:
			return
		}
	}
}
