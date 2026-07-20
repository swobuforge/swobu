package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
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
		tui.OnStop(tui.KeyDown, SelectNext),
		tui.OnStop(tui.KeyUp, SelectPrevious),
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

func TestEditableRow_HelperTextUsesCallerOwnedCopy(t *testing.T) {
	value := tui.NewState("")
	row := NewEditableRow("slug", "slug", value)
	row.Validation = EditableRowValidationRequired

	if got := row.HelperText(); got != "" {
		t.Fatalf("helper text without caller copy = %q, want empty", got)
	}

	row.ValidationText = " enter a workspace name "
	if got := row.HelperText(); got != "enter a workspace name" {
		t.Fatalf("helper text = %q, want caller-owned copy", got)
	}
}

func TestEditableRow_HelperLineAlignsWithValueColumn(t *testing.T) {
	value := tui.NewState("")
	row := NewEditableRow("slug", "slug", value)
	row.Validation = EditableRowValidationRequired
	row.ValidationText = "enter a workspace name"

	rendered := testkit.RenderMountedTrimmed(t, row, 90, 4)
	lines := strings.Split(rendered, "\n")
	if len(lines) < 2 {
		t.Fatalf("frame missing helper line:\n%s", rendered)
	}
	wantPrefix := strings.Repeat(" ", 20) + "enter a workspace name"
	if !strings.HasPrefix(lines[1], wantPrefix) {
		t.Fatalf("helper line misaligned:\n%s\nwant prefix %q", rendered, wantPrefix)
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

func TestEditableRow_EditModeDoesNotTraverseAwayOnDown(t *testing.T) {
	value := tui.NewState("dev")
	row := NewEditableRow("slug", "slug", value)
	after := NewSelectableRow("after", "after", "", "", nil)

	root := newEditableHarnessRoot(NewSelectableRow("wrap", "wrap", "", "", nil), row, after)
	h := makeEditableHarness(t, root)

	focusRow(t, h)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	frame := h.Frame()
	if !strings.Contains(frame, "> slug") || strings.Contains(frame, "> after") {
		t.Fatalf("edit mode Down should keep global selection on editable row:\n%s", frame)
	}
	if !row.IsEditing() {
		t.Fatal("row should remain in edit mode after Down")
	}
}

func TestEditableRow_StartEditingMountsInEditMode(t *testing.T) {
	value := tui.NewState("")
	row := NewEditableRow("secret", "secret", value)
	row.StartEditing = true
	row.AutoFocus = true

	h := makeEditableHarness(t, newEditableHarnessRoot(row))
	rendered := h.Frame()

	testkit.AssertFocusedFrame(t, rendered, "> secret")
	if !strings.Contains(rendered, "save ↵") {
		t.Fatalf("frame missing edit action:\n%s", rendered)
	}
	if !strings.Contains(rendered, "_") {
		t.Fatalf("frame missing input cursor:\n%s", rendered)
	}
}

func TestEditableRow_StartEditingMountFocusesRowElement(t *testing.T) {
	value := tui.NewState("")
	row := NewEditableRow("secret", "secret", value)
	row.StartEditing = true
	after := NewSelectableRow("after", "after", "", "", nil)

	h := makeEditableHarness(t, newEditableHarnessRoot(row, after))
	if row.Ref().El() == nil || !row.Ref().El().IsFocused() {
		t.Fatalf("StartEditing row should own actual focus:\n%s", h.Frame())
	}
}

func TestEditableRow_RenderEditDoesNotMutateFocusOrState(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "editable_row.go", nil, 0)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	fn := methodDecl(file, "EditableRow", "renderEdit")
	if fn == nil {
		t.Fatal("missing EditableRow.renderEdit")
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Focus", "FocusNow", "Set":
			pos := fset.Position(call.Pos())
			t.Fatalf("renderEdit must stay render-pure; found %s call at %s", sel.Sel.Name, pos)
		}
		return true
	})
}

func methodDecl(file *ast.File, receiver, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != name {
			continue
		}
		for _, recv := range fn.Recv.List {
			if receiverTypeName(recv.Type) == receiver {
				return fn
			}
		}
	}
	return nil
}

func receiverTypeName(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return receiverTypeName(v.X)
	default:
		return ""
	}
}

func TestEditableRow_TypingStaysDraftUntilSubmit(t *testing.T) {
	value := tui.NewState("")
	row := NewEditableRow("slug", "slug", value)

	root := newEditableHarnessRoot(NewSelectableRow("wrap", "wrap", "", "", nil), row)
	h := makeEditableHarness(t, root)

	focusRow(t, h)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter}) // open editor
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'x'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'y'})

	if got := value.Get(); got != "" {
		t.Fatalf("durable value changed before submit: %q", got)
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
	if got := value.Get(); got != "dev" {
		t.Fatalf("published value = %q, want dev", got)
	}
}

// freshEditableRowRoot mounts a freshly-built EditableRow on every render,
// mirroring production render helpers such as RouteNameRowComponent that rebuild
// the row with a brand-new value state each frame. The mounted instance is
// preserved across renders (so edit/typing state survives) while the props value
// pointer is repointed via UpdateProps — the exact condition that exposed the
// stale-submit bug.
type freshEditableRowRoot struct {
	build func() *EditableRow
}

func (r *freshEditableRowRoot) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100.00),
	)
	wrap := NewSelectableRow("wrap", "wrap", "", "", nil)
	root.AddChild(app.Mount(r, 0, func() tui.Component { return wrap }))
	root.AddChild(app.Mount(r, 1, func() tui.Component { return r.build() }))
	return root
}

func (r *freshEditableRowRoot) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyDown, SelectNext),
		tui.OnStop(tui.KeyUp, SelectPrevious),
	}
}

// enteredEditableRemountRoot models a semantic owner replacement after the app
// has already applied selection elsewhere. The replacement editor must own
// actual dispatch selection, not only paint an entered marker.
type enteredEditableRemountRoot struct {
	showEditor bool
	submitted  string
}

func (r *enteredEditableRemountRoot) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)
	if !r.showEditor {
		root.AddChild(app.Mount(r, "before", func() tui.Component {
			return NewSelectableRow("before", "before", "", "", nil)
		}))
		return root
	}
	root.AddChild(app.Mount(r, "replacement-before", func() tui.Component {
		return NewSelectableRow("replacement-before", "replacement before", "", "", nil)
	}))
	root.AddChild(app.Mount(r, "editor", func() tui.Component {
		row := NewEditableRow("editor", "name", tui.NewState(""))
		row.Open()
		row.OnSubmit = func(value string) { r.submitted = value }
		return row
	}))
	return root
}

func TestEditableRow_EnteredRemountOwnsDispatchSelection(t *testing.T) {
	root := &enteredEditableRemountRoot{}
	h := makeEditableHarness(t, root)
	h.FocusNext()

	root.showEditor = true
	if frame := h.Frame(); strings.Count(frame, "> ") != 1 {
		t.Fatalf("entered remount must replace sibling selection:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'd'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if root.submitted != "d" {
		t.Fatalf("entered remount submitted %q, want d; painted entry must own dispatch selection", root.submitted)
	}
}

// TestEditableRow_SubmitSendsTypedValueAcrossRenders guards against the
// reconciliation divergence: when the row is rebuilt with a fresh value state on
// each render, the submitted value must still be what the operator typed, not the
// stale value state left on the row shell.
func TestEditableRow_SubmitSendsTypedValueAcrossRenders(t *testing.T) {
	var submitted string
	build := func() *EditableRow {
		row := NewEditableRow("slug", "slug", tui.NewState("dev"))
		row.OnSubmit = func(s string) { submitted = s }
		return row
	}

	h := makeEditableHarness(t, &freshEditableRowRoot{build: build})

	focusRow(t, h)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})           // open editor
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'x'}) // type
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})           // submit

	if submitted != "devx" {
		t.Fatalf("submitted = %q, want %q (typed value must survive render reconciliation)", submitted, "devx")
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
