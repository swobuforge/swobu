package ui

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
)

// Verify that Escape while in edit mode returns the row to view mode
// and the cursor disappears from the frame.
func TestEditableRow_EscapeFromEditReturnsToView(t *testing.T) {
	value := tui.NewState("dev")
	row := NewEditableRow("slug", "slug", value)

	root := newEditableHarnessRoot(NewSelectableRow("wrap", "wrap", "", "", nil), row)
	h := makeEditableHarness(t, root)

	focusRow(t, h)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // open editor
	frame := h.Frame()
	if !strings.Contains(frame, "save ↵") {
		t.Fatalf("expected edit mode action, got:\n%s", frame)
	}
	if !strings.Contains(frame, "dev") {
		t.Fatalf("expected value 'dev' in edit frame, got:\n%s", frame)
	}

	// Escape should return to view mode
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	frame = h.Frame()
	if !strings.Contains(frame, "edit ↵") {
		t.Fatalf("expected view mode action after Escape, got:\n%s", frame)
	}
	if strings.Contains(frame, "▌") {
		t.Fatalf("cursor should disappear after Escape, got:\n%s", frame)
	}
	if row.IsEditing() {
		t.Fatal("row should be in view mode after Escape")
	}
}

// Verify that after Escape the row element still has focus.
func TestEditableRow_EscapeKeepsFocusOnRow(t *testing.T) {
	value := tui.NewState("dev")
	row := NewEditableRow("slug", "slug", value)

	root := newEditableHarnessRoot(NewSelectableRow("wrap", "wrap", "", "", nil), row)
	h := makeEditableHarness(t, root)

	focusRow(t, h)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // open editor, row focused
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	// The row ref should still point to the focused element
	if row.Ref.El() == nil {
		t.Fatal("row ref not resolved after Escape")
	}
	if !row.Ref.El().IsFocused() {
		t.Fatal("row element should still be focused after Escape")
	}
}

// Verify that typing while in edit mode updates the value state.
func TestEditableRow_TypingInEditModeUpdatesValue(t *testing.T) {
	value := tui.NewState("")
	row := NewEditableRow("slug", "slug", value)

	root := newEditableHarnessRoot(NewSelectableRow("wrap", "wrap", "", "", nil), row)
	h := makeEditableHarness(t, root)

	focusRow(t, h)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // open editor
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'x'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'y'})

	if got := value.Get(); got != "xy" {
		t.Fatalf("value after typing = %q, want xy", got)
	}
	frame := h.Frame()
	if !strings.Contains(frame, "xy▌") && !strings.Contains(frame, "xy ") {
		// Cursor may be on or off depending on blink phase; value must be there.
		if !strings.Contains(frame, "xy") {
			t.Fatalf("frame should show typed value 'xy', got:\n%s", frame)
		}
	}
}

// Verify that Enter while in edit mode calls OnSubmit and value is passed.
func TestEditableRow_EnterInEditMode_Submits(t *testing.T) {
	var submitted string
	value := tui.NewState("prod")
	row := NewEditableRow("slug", "slug", value)
	row.OnSubmit = func(s string) {
		submitted = s
	}

	root := newEditableHarnessRoot(NewSelectableRow("wrap", "wrap", "", "", nil), row)
	h := makeEditableHarness(t, root)

	focusRow(t, h)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // open
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // submit

	if submitted != "prod" {
		t.Fatalf("submitted = %q, want prod", submitted)
	}
}

// Verify the row still shows the active-descendant arrow in edit mode.
func TestEditableRow_EditModeShowsActiveDescendantArrow(t *testing.T) {
	value := tui.NewState("test")
	row := NewEditableRow("slug", "slug", value)

	root := newEditableHarnessRoot(NewSelectableRow("wrap", "wrap", "", "", nil), row)
	h := makeEditableHarness(t, root)

	focusRow(t, h)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	frame := h.Frame()
	if !strings.Contains(frame, "> slug") {
		t.Fatalf("edit mode should still show '> slug', got:\n%s", frame)
	}
}
