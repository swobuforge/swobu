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
		if app != nil {
			root.AddChild(app.Mount(r, idx, func() tui.Component { return child }))
			continue
		}
		root.AddChild(child.Render(nil))
	}

	return root
}

func (r *selectHarnessRoot) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyDown, MoveNext),
		tui.OnStop(tui.KeyUp, MovePrev),
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

func assertFocusedRow(t *testing.T, frame, label string) {
	t.Helper()
	if !strings.Contains(frame, "> "+label) {
		t.Fatalf("frame missing focused row %q:\n%s", label, frame)
	}
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

func TestActivateFocused_BindsEnterAndSpace(t *testing.T) {
	var calls int
	keymap := ActivateFocused(func(tui.KeyEvent) {
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

func TestSelectBase_FocusRepairUsesTraversal(t *testing.T) {
	root := newSelectHarnessRoot(selectRows("alpha", "beta")...)
	h := makeSelectHarness(t, root)

	if got := h.Frame(); !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("frame missing expected rows:\n%s", got)
	}

	h.FocusNext()
	assertFocusedRow(t, h.Frame(), "alpha")

	root.rows[1].Focus(h.App())
	flushApp(t, h)
	assertFocusedRow(t, h.Frame(), "beta")

	FocusRefByTraversal(h.App(), root.rows[0].Ref)
	flushApp(t, h)
	assertFocusedRow(t, h.Frame(), "alpha")
}

func TestFocusFirstByTraversal_SkipsNilRefs(t *testing.T) {
	root := newSelectHarnessRoot(selectRows("alpha", "beta", "gamma")...)
	h := makeSelectHarness(t, root)

	h.Frame()
	h.FocusNext()
	assertFocusedRow(t, h.Frame(), "alpha")

	focusFirstByTraversal(h.App(), root.rows[2].Ref, root.rows[1].Ref)
	flushApp(t, h)
	assertFocusedRow(t, h.Frame(), "gamma")
}

func TestFocusRefByTraversal_IgnoresMissingRef(t *testing.T) {
	root := newSelectHarnessRoot(selectRows("alpha", "beta")...)
	h := makeSelectHarness(t, root)

	h.Frame()
	h.FocusNext()
	assertFocusedRow(t, h.Frame(), "alpha")

	missing := tui.NewRef()
	FocusRefByTraversal(h.App(), missing)
	flushApp(t, h)
	assertFocusedRow(t, h.Frame(), "alpha")
}

func TestMoveNextAndPrev_AdvanceFocus(t *testing.T) {
	root := newSelectHarnessRoot(selectRows("alpha", "beta")...)
	h := makeSelectHarness(t, root)

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	assertFocusedRow(t, h.Frame(), "alpha")

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	assertFocusedRow(t, h.Frame(), "beta")

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	assertFocusedRow(t, h.Frame(), "alpha")
}

func TestSelectGrammarConstants(t *testing.T) {
	if got, want := SelectArrowFocused, ">"; got != want {
		t.Fatalf("SelectArrowFocused = %q, want %q", got, want)
	}
	if got, want := SelectArrowBlurred, " "; got != want {
		t.Fatalf("SelectArrowBlurred = %q, want %q", got, want)
	}
}

func focusFirstByTraversal(app *tui.App, refs ...*tui.Ref) {
	if app == nil {
		return
	}

	app.QueueUpdate(func() {
		for _, ref := range refs {
			if ref == nil {
				continue
			}

			if el := ref.El(); el != nil && focusElementByTraversal(app, el) {
				return
			}
		}
	})
}
