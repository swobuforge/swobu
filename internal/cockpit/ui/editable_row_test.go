package ui

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

type editableHarnessRoot struct {
	children []tui.Component
}

func newEditableHarnessRoot(children ...tui.Component) *editableHarnessRoot {
	return &editableHarnessRoot{children: children}
}

func (r *editableHarnessRoot) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100.00),
	)
	for i, child := range r.children {
		idx := i
		c := child
		root.AddChild(app.Mount(r, idx, func() tui.Component { return c }))
	}
	return root
}

func (r *editableHarnessRoot) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyDown, MoveNext),
		tui.OnStop(tui.KeyUp, MovePrev),
	}
}

func makeEditableHarness(t *testing.T, root tui.Component) *testkit.MockAppHarness {
	t.Helper()
	h, err := testkit.NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	h.Open()
	t.Cleanup(h.Close)
	return h
}

func TestEditableRow_ViewModeRendersArrowLabelValueAction(t *testing.T) {
	value := tui.NewState("dev")
	row := NewEditableRow("slug", "slug", value)

	rendered := testkit.RenderMountedTrimmed(t, row, 90, 4)
	if !strings.Contains(rendered, " slug") {
		t.Fatalf("frame missing label:\n%s", rendered)
	}
	if !strings.Contains(rendered, "dev") {
		t.Fatalf("frame missing value:\n%s", rendered)
	}
	if !strings.Contains(rendered, "edit ↵") {
		t.Fatalf("frame missing view action:\n%s", rendered)
	}
}

func focusRow(t *testing.T, h *testkit.MockAppHarness) {
	t.Helper()
	// wrap is at index 0, row at index 1; need two downs
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
}

func TestEditableRow_FocusShowsArrowActivateOpensInput(t *testing.T) {
	value := tui.NewState("dev")
	row := NewEditableRow("slug", "slug", value)

	root := newEditableHarnessRoot(NewSelectableRow("wrap", "wrap", "", "", nil), row)
	h := makeEditableHarness(t, root)

	frame := h.Frame()
	testkit.AssertUnfocusedFrame(t, frame, "> slug")

	testkit.AssertFocusVisible(t, h, func() {
		focusRow(t, h)
	}, "> slug")
	frame = h.Frame()
	if !strings.Contains(frame, "edit ↵") {
		t.Fatalf("frame missing view action pre-activate:\n%s", frame)
	}

	// Activate the focused row — should switch to edit mode
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	frame = h.Frame()
	testkit.AssertFocusedFrame(t, frame, "> slug")
	if !strings.Contains(frame, "save ↵") {
		t.Fatalf("frame missing edit action post-activate:\n%s", frame)
	}
	// The input is mounted and autoFocused; cursor should appear
	if !strings.Contains(frame, "_") {
		t.Fatalf("frame missing input cursor in edit mode:\n%s", frame)
	}
}

func TestEditableRow_TypingUpdatesValueState(t *testing.T) {
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
}

func TestEditableRow_SubmitFiresOnSubmit(t *testing.T) {
	var submitted string
	value := tui.NewState("dev")
	row := NewEditableRow("slug", "slug", value)
	row.OnSubmit = func(s string) {
		submitted = s
	}

	root := newEditableHarnessRoot(NewSelectableRow("wrap", "wrap", "", "", nil), row)
	h := makeEditableHarness(t, root)

	focusRow(t, h)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // open editor
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // submit

	if submitted != "dev" {
		t.Fatalf("submitted = %q, want dev", submitted)
	}
}

func TestEditableRow_CustomOnActivateBypassesAutoOpen(t *testing.T) {
	var activated bool
	value := tui.NewState("dev")
	row := NewEditableRow("slug", "slug", value)
	row.OnActivate = func() {
		activated = true
	}

	root := newEditableHarnessRoot(NewSelectableRow("wrap", "wrap", "", "", nil), row)
	h := makeEditableHarness(t, root)

	focusRow(t, h)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if !activated {
		t.Fatal("custom OnActivate should fire")
	}
	// With custom OnActivate, Open is not auto-called; row stays in view mode
	if row.IsEditing() {
		t.Fatal("row should not auto-open when OnActivate is set")
	}
}
