package ui

import (
	"strings"
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

type selectHarnessRoot struct {
	rows []*SelectableRow
}

func newSelectHarnessRoot(rows ...*SelectableRow) *selectHarnessRoot {
	return &selectHarnessRoot{rows: rows}
}

func (r *selectHarnessRoot) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100.00),
	)

	for i, row := range r.rows {
		idx := i
		child := row
		root.AddChild(app.Mount(r, idx, func() tui.Component { return child }))
	}

	return root
}

func (r *selectHarnessRoot) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyDown, SelectNext),
		tui.OnStop(tui.KeyUp, SelectPrevious),
	}
}

func makeSelectHarness(t *testing.T, root tui.Component) *testkit.MockAppHarness {
	t.Helper()

	h, err := testkit.NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	h.Open()
	t.Cleanup(h.Close)
	return h
}

func selectRows(labels ...string) []*SelectableRow {
	rows := make([]*SelectableRow, 0, len(labels))
	for i, label := range labels {
		rows = append(rows, NewSelectableRow(
			"row."+strings.ReplaceAll(label, " ", "-")+"."+string(rune('a'+i)),
			label,
			"",
			"select",
			func() {},
		))
	}
	return rows
}

func bindingForKey(t *testing.T, keymap tui.KeyMap, key tui.Key) tui.KeyBinding {
	t.Helper()

	for _, binding := range keymap {
		if binding.Pattern.Key == key {
			return binding
		}
	}

	t.Fatalf("keymap missing binding for %v", key)
	return tui.KeyBinding{}
}

func bindingForRune(t *testing.T, keymap tui.KeyMap, r rune) tui.KeyBinding {
	t.Helper()

	for _, binding := range keymap {
		if binding.Pattern.Rune == r {
			return binding
		}
	}

	t.Fatalf("keymap missing binding for %q", r)
	return tui.KeyBinding{}
}

func flushApp(t *testing.T, h *testkit.MockAppHarness) {
	t.Helper()

	deadline := time.NewTimer(500 * time.Millisecond)
	defer deadline.Stop()

	for {
		select {
		case ev := <-h.App().Events():
			h.App().Dispatch(ev)
			h.App().Render()
			if _, ok := ev.(tui.UpdateEvent); ok {
				return
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for queued update")
		}
	}
}

func TestActivateSelected_BindsEnterAndSpace(t *testing.T) {
	var calls int
	keymap := ActivateSelected(func(tui.KeyEvent) {
		calls++
	})

	for _, key := range []tui.Key{tui.KeyEnter, tui.KeyRune} {
		var binding tui.KeyBinding
		if key == tui.KeyEnter {
			binding = bindingForKey(t, keymap, key)
		} else {
			binding = bindingForRune(t, keymap, ' ')
		}
		if !binding.Pattern.FocusRequired {
			t.Fatalf("binding for %v should require focus", key)
		}
	}

	bindingForKey(t, keymap, tui.KeyEnter).Handler(tui.KeyEvent{Key: tui.KeyEnter})
	bindingForRune(t, keymap, ' ').Handler(tui.KeyEvent{Key: tui.KeyRune, Rune: ' '})

	if got, want := calls, 2; got != want {
		t.Fatalf("activate calls = %d, want %d", got, want)
	}
}

func TestSelectableRow_FocusRepairUsesInteractionTraversal(t *testing.T) {
	root := newSelectHarnessRoot(selectRows("alpha", "beta")...)
	h := makeSelectHarness(t, root)

	if got := h.Frame(); !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("frame missing expected rows:\n%s", got)
	}

	h.FocusNext()
	testkit.AssertFocusedFrame(t, h.Frame(), "> alpha")

	root.rows[1].Focus(h.App())
	testkit.AssertFocusedFrame(t, h.Frame(), "> beta")

	root.rows[0].Focus(h.App())
	testkit.AssertFocusedFrame(t, h.Frame(), "> alpha")
}

func TestSelectNextAndPrevious_AdvanceSelection(t *testing.T) {
	root := newSelectHarnessRoot(selectRows("alpha", "beta")...)
	h := makeSelectHarness(t, root)

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	testkit.AssertFocusedFrame(t, h.Frame(), "> alpha")

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	testkit.AssertFocusedFrame(t, h.Frame(), "> beta")

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	testkit.AssertFocusedFrame(t, h.Frame(), "> alpha")
}

func TestSelectGrammarConstants(t *testing.T) {
	if got, want := SelectArrowFocused, ">"; got != want {
		t.Fatalf("SelectArrowFocused = %q, want %q", got, want)
	}
	if got, want := SelectArrowBlurred, " "; got != want {
		t.Fatalf("SelectArrowBlurred = %q, want %q", got, want)
	}
}

func TestRowArrow(t *testing.T) {
	if got, want := RowArrow(true), ">"; got != want {
		t.Fatalf("RowArrow(true) = %q, want %q", got, want)
	}
	if got, want := RowArrow(false), " "; got != want {
		t.Fatalf("RowArrow(false) = %q, want %q", got, want)
	}
}

func TestSelectableRow_AutoFocusSeedsVisibleArrow(t *testing.T) {
	row := NewSelectableRow("row.auto", "picker option", "alpha", "select", nil)
	row.AutoFocus = true
	root := newSelectHarnessRoot(row)
	h := makeSelectHarness(t, root)

	testkit.AssertFocusedFrame(t, h.Frame(), "> picker option")
}

func TestSelectableRow_ActivateUpdatesVisibleAction(t *testing.T) {
	row := NewSelectableRow("row.copy", "client base URL", "http://127.0.0.1:7926/c/dev", "copy ↵", nil)
	row.Activate = func() {
		row.Action = "copied"
	}
	root := newSelectHarnessRoot(row)
	h := makeSelectHarness(t, root)

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	testkit.AssertFocusedFrame(t, h.Frame(), "> client base URL")
	if !strings.Contains(h.Frame(), "copy ↵") {
		t.Fatalf("frame missing initial action:\n%s", h.Frame())
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame := h.Frame()
	if !strings.Contains(frame, "copied") {
		t.Fatalf("frame missing updated action:\n%s", frame)
	}
}

func TestSelectableRow_LongValueKeepsActionSeparated(t *testing.T) {
	row := NewSelectableRow(
		"row.copy",
		"client base URL",
		"http://127.0.0.1:46355/c/acme-clients",
		"copy ↵",
		nil,
	)
	root := newSelectHarnessRoot(row)
	h := makeSelectHarness(t, root)

	h.FocusNext()
	testkit.AssertFocusedFrame(t, h.Frame(), "> client base URL")

	frame := h.Frame()
	if strings.Contains(frame, "acme-clientscopy") {
		t.Fatalf("frame still glues value to action:\n%s", frame)
	}
	if !strings.Contains(frame, "copy ↵") {
		t.Fatalf("frame missing action label:\n%s", frame)
	}
}

func TestSelectableRow_UpdatePropsRefreshesActionAndCallback(t *testing.T) {
	row := NewSelectableRow("row.copy", "client base URL", "http://127.0.0.1:7926/c/dev", "copy ↵", func() {})

	called := false
	fresh := NewSelectableRow("row.copy", "client base URL", "http://127.0.0.1:7926/c/dev", "copied", func() {
		called = true
	})

	row.UpdateProps(fresh)

	if got, want := row.Action, "copied"; got != want {
		t.Fatalf("row.Action = %q, want %q", got, want)
	}
	if got, want := row.Value, "http://127.0.0.1:7926/c/dev"; got != want {
		t.Fatalf("row.Value = %q, want %q", got, want)
	}

	row.Activate()
	if !called {
		t.Fatal("UpdateProps did not refresh the activation callback")
	}
}
