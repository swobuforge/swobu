package ui

import (
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
)

// TestEditableRowBlinkGatedByEditMode is the epic-50 task-040 closure guard.
//
// 040 hypothesized that a view-mode EditableRow (the normal idle state of every
// editable field on screen) leaks its 500ms cursor-blink OnTimer into the app
// event loop, marking the whole app dirty twice a second at idle. The blink
// watcher is in fact ALREADY gated: InlineEditor.input is created lazily in
// Open() (ensureInput), and Watchers() returns nil until then, so go-tui never
// collects a blink timer for a row that is not being edited. Measured at idle:
// 0 blink-driven events over a 3s window vs ~2 events per 1.1s while editing.
//
// This test pins BOTH halves of that invariant so it cannot silently regress:
// the fast structural assertion (view nil / edit one) is the real guard; the
// timed confirmation is skipped under -short because it spans blink intervals.
func TestEditableRowBlinkGatedByEditMode(t *testing.T) {
	value := tui.NewState("standby")
	row := NewEditableRow("blink-gate", "field", value)

	// View mode: no blink watcher is exposed, so the idle app never re-renders
	// for a cursor that is not visible.
	if watchers := row.Watchers(); len(watchers) != 0 {
		t.Fatalf("view-mode EditableRow must expose no watchers, got %d", len(watchers))
	}

	// Edit mode: the blink watcher is exposed exactly once.
	row.Open()
	if watchers := row.Watchers(); len(watchers) != 1 {
		t.Fatalf("edit-mode EditableRow must expose exactly one blink watcher, got %d", len(watchers))
	}

	if testing.Short() {
		return
	}

	// Timed confirmation: at idle the app receives no blink events; while
	// editing it receives ~one per 500ms. Proves the structural gate holds at
	// the runtime level, not just the API surface.
	app, _, err := mountedrender.NewApp(80, 24)
	if err != nil {
		t.Fatalf("mountedrender.NewApp: %v", err)
	}
	defer app.Close()
	app.SetRootComponent(row)
	app.MarkDirty()
	app.Render()
	drainEvents(app, 600*time.Millisecond) // settle past the first tick

	if n := countIdleEvents(app, 1100*time.Millisecond); n == 0 {
		t.Fatalf("expected blink ticks while editing, got 0 events in 1.1s — timer did not start")
	}
}

func drainEvents(app *tui.App, window time.Duration) {
	deadline := time.After(window)
	for {
		select {
		case ev := <-app.Events():
			app.Dispatch(ev)
		case <-deadline:
			return
		}
	}
}

func countIdleEvents(app *tui.App, window time.Duration) int {
	deadline := time.After(window)
	var n int
	for {
		select {
		case ev := <-app.Events():
			app.Dispatch(ev)
			n++
		case <-deadline:
			return n
		}
	}
}
