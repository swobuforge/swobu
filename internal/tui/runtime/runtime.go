package runtime

import (
	"github.com/swobuforge/swobu/internal/tui/compiler"
	"github.com/swobuforge/swobu/internal/tui/core"
)

// ── Loop ─────────────────────────────────────────────────────────────

// Loop drives a core.App[S,E] through the compiler and a fake terminal.
type Loop[S any, E any] struct {
	App           core.App[S, E]
	Comp          *compiler.Compiler[E]
	Term          *FakeTerminal
	State         S
	Focus         core.FocusMemory
	Size          core.Size
	OnEscCollapse func(core.Key) E // called when Esc has no direct cancel route
}

// NewLoop creates a runtime loop with a fake terminal.
func NewLoop[S any, E any](app core.App[S, E], term *FakeTerminal) *Loop[S, E] {
	return &Loop[S, E]{
		App:   app,
		Comp:  compiler.New[E](),
		Term:  term,
		Size:  term.Size,
		Focus: core.FocusMemory{},
	}
}

// Init bootstraps the app and compiles the first frame.
func (l *Loop[S, E]) Init() error {
	initState, effects := l.App.Init()
	l.State = initState
	for _, e := range effects {
		_ = e // skeleton: ignore effects on init
	}
	return l.render()
}

func (l *Loop[S, E]) compile() compiler.CompiledFrame[E] {
	return l.Comp.Compile(compiler.CompileInput[E]{
		Root:  l.App.View(l.State),
		Size:  l.Size,
		Focus: l.Focus,
	})
}

// activeFocus returns the current focus ID, priming it on first use.
func (l *Loop[S, E]) activeFocus(compiled compiler.CompiledFrame[E]) core.FocusID {
	if l.Focus.ActiveFocusID == "" {
		if first, ok := compiled.FocusGraph.FirstEnabled(); ok {
			l.Focus.ActiveFocusID = first.ID
		}
	}
	return l.Focus.ActiveFocusID
}

// SendInput translates a raw input string into an intent and dispatches it.
func (l *Loop[S, E]) SendInput(input string) (E, bool) {
	var zeroE E
	compiled := l.compile()

	intent, ok := compiler.DefaultKeymap().Resolve(input)
	if !ok {
		return zeroE, false
	}

	// Global shortcuts fire regardless of focus or disabled status.
	if shortcut, ok := compiled.InteractionRoutes.FindGlobalShortcut(intent); ok {
		l.State, _ = l.App.Update(l.State, shortcut.Signal.Event)
		l.render()
		return shortcut.Signal.Event, true
	}

	// Quit is a runtime-level global intent (no app event).
	if intent == core.IntentQuit {
		return zeroE, false
	}

	// Scoped body focus movement and activation.
	focusID := l.Focus.ActiveFocusID
	switch intent {
	case core.IntentMoveNext:
		if focusID == "" {
			if first, ok := compiled.FocusGraph.FirstEnabled(); ok {
				l.Focus.ActiveFocusID = first.ID
			}
		} else {
			if next, ok := compiled.FocusGraph.NextEnabled(focusID); ok {
				l.Focus.ActiveFocusID = next.ID
			}
		}
		l.render()
		return zeroE, true
	case core.IntentMovePrevious:
		if focusID == "" {
			if last, ok := compiled.FocusGraph.LastEnabled(); ok {
				l.Focus.ActiveFocusID = last.ID
			}
		} else {
			if prev, ok := compiled.FocusGraph.PrevEnabled(focusID); ok {
				l.Focus.ActiveFocusID = prev.ID
			}
		}
		l.render()
		return zeroE, true
	case core.IntentCancel:
		return l.handleCancel(compiled)
	}

	// Scoped activation routes.
	focusID = l.activeFocus(compiled)
	route, ok := compiled.InteractionRoutes.Find(intent, focusID)
	if !ok {
		return zeroE, false
	}
	return l.dispatchRoute(route)
}

// SendIntent dispatches a specific intent directly. Used by tests for body
// focus movement when the default keymap uses Tab for tab-rail switching.
func (l *Loop[S, E]) SendIntent(intent core.Intent) (E, bool) {
	var zeroE E
	compiled := l.compile()
	focusID := l.Focus.ActiveFocusID
	switch intent {
	case core.IntentMoveNext:
		if focusID == "" {
			if first, ok := compiled.FocusGraph.FirstEnabled(); ok {
				l.Focus.ActiveFocusID = first.ID
			}
		} else {
			if next, ok := compiled.FocusGraph.NextEnabled(focusID); ok {
				l.Focus.ActiveFocusID = next.ID
			}
		}
		l.render()
		return zeroE, true
	case core.IntentMovePrevious:
		if focusID == "" {
			if last, ok := compiled.FocusGraph.LastEnabled(); ok {
				l.Focus.ActiveFocusID = last.ID
			}
		} else {
			if prev, ok := compiled.FocusGraph.PrevEnabled(focusID); ok {
				l.Focus.ActiveFocusID = prev.ID
			}
		}
		l.render()
		return zeroE, true
	}
	focusID = l.activeFocus(compiled)
	route, ok := compiled.InteractionRoutes.Find(intent, focusID)
	if !ok {
		return zeroE, false
	}
	return l.dispatchRoute(route)
}

func (l *Loop[S, E]) handleCancel(compiled compiler.CompiledFrame[E]) (E, bool) {
	var zeroE E
	focusID := l.activeFocus(compiled)

	// First try: dispatch cancel on the focused target.
	route, ok := compiled.InteractionRoutes.Find(core.IntentCancel, focusID)
	if ok {
		return l.dispatchRoute(route)
	}

	// Second try: collapse nearest expanded ancestor.
	if l.OnEscCollapse == nil {
		return zeroE, false
	}
	root := l.App.View(l.State)
	var targetKey core.Key
	for _, t := range compiled.FocusGraph.Targets {
		if t.ID == focusID {
			targetKey = t.Key
			break
		}
	}
	if targetKey.Empty() {
		return zeroE, false
	}
	ancestorKey, ok := findNearestExpandedAncestor(root, targetKey)
	if !ok {
		return zeroE, false
	}
	ev := l.OnEscCollapse(ancestorKey)
	l.State, _ = l.App.Update(l.State, ev)
	l.render()
	return ev, true
}

// SendFieldEvent dispatches a field-level intent for the currently focused
// field node. The intent may be edit, activate (commit), or cancel.
func (l *Loop[S, E]) SendFieldEvent(intent core.Intent, value string) (E, bool) {
	var zeroE E
	compiled := l.compile()
	focusID := l.activeFocus(compiled)

	route, ok := compiled.InteractionRoutes.Find(intent, focusID)
	if !ok {
		return zeroE, false
	}

	var ev E
	var dispatched bool
	switch intent {
	case core.IntentEdit:
		if route.FieldChange != nil {
			ev = route.FieldChange(value)
			dispatched = true
		}
	case core.IntentBackspace:
		if route.FieldChange != nil {
			ev = route.FieldChange(value)
			dispatched = true
		}
	case core.IntentActivate:
		if route.FieldCommit != nil {
			ev = route.FieldCommit(value)
			dispatched = true
		}
	case core.IntentCancel:
		if route.FieldCancel != nil {
			ev = route.FieldCancel()
			dispatched = true
		}
	}
	if dispatched {
		l.State, _ = l.App.Update(l.State, ev)
		l.render()
		return ev, true
	}
	return zeroE, false
}

func findNearestExpandedAncestor[E any](root core.Node[E], key core.Key) (core.Key, bool) {
	var path []core.Node[E]
	var found bool
	var walk func(core.Node[E])
	walk = func(n core.Node[E]) {
		if found {
			return
		}
		if n.Key() == key {
			found = true
			return
		}
		path = append(path, n)
		for _, ch := range n.Children() {
			walk(ch)
			if found {
				return
			}
		}
		path = path[:len(path)-1]
	}
	walk(root)
	if !found {
		return core.Key(""), false
	}
	for i := len(path) - 1; i >= 0; i-- {
		if path[i].Kind() == "region" && path[i].IsExpanded() {
			return path[i].Key(), true
		}
	}
	return core.Key(""), false
}

func (l *Loop[S, E]) dispatchRoute(route compiler.InteractionRoute[E]) (E, bool) {
	var zeroE E
	if len(route.Signals) > 0 {
		l.State, _ = l.App.Update(l.State, route.Signals[0].Event)
		l.render()
		return route.Signals[0].Event, true
	}
	return zeroE, false
}

func (l *Loop[S, E]) render() error {
	l.Term.lastFrame = l.compile().Frame
	return nil
}

// ── FakeTerminal ─────────────────────────────────────────────────────

// FakeTerminal records the last committed frame and supports introspection.
type FakeTerminal struct {
	Size      core.Size
	lastFrame core.Frame
}

// NewFakeTerminal creates a fake terminal of the given dimensions.
func NewFakeTerminal(w, h int) *FakeTerminal {
	return &FakeTerminal{Size: core.Size{W: w, H: h}}
}

// LastFrame returns the most recently committed frame.
func (ft *FakeTerminal) LastFrame() core.Frame { return ft.lastFrame }

// RenderText returns a simple text representation of the frame for assertions.
func (ft *FakeTerminal) RenderText() string {
	out := make([]rune, 0, ft.Size.W*ft.Size.H)
	for y := 0; y < ft.Size.H; y++ {
		for x := 0; x < ft.Size.W; x++ {
			out = append(out, ft.cellRune(x, y))
		}
		if y < ft.Size.H-1 {
			out = append(out, '\n')
		}
	}
	return string(out)
}

// RowText returns the text content of a specific row (trimmed of trailing spaces).
func (ft *FakeTerminal) RowText(y int) string {
	if y < 0 || y >= ft.Size.H {
		return ""
	}
	row := make([]rune, 0, ft.Size.W)
	for x := 0; x < ft.Size.W; x++ {
		row = append(row, ft.cellRune(x, y))
	}
	for len(row) > 0 && row[len(row)-1] == ' ' {
		row = row[:len(row)-1]
	}
	return string(row)
}

func (ft *FakeTerminal) cellRune(x, y int) rune {
	idx := y*ft.Size.W + x
	if idx >= len(ft.lastFrame.Cells) {
		return ' '
	}
	r := ft.lastFrame.Cells[idx].Rune
	if r == 0 {
		return ' '
	}
	return r
}

// Snapshot is a serializable summary of compiler/runtime state.
type Snapshot struct {
	VisibleLines []compiler.VisibleLine
	FocusGraph   compiler.FocusGraph
	FocusMemory  core.FocusMemory
	FrameSize    core.Size
	FrameRows    []string
}

// TakeSnapshot builds a snapshot from the current loop state.
func TakeSnapshot[S any, E any](l *Loop[S, E]) Snapshot {
	compiled := l.compile()
	rows := make([]string, 0, compiled.Frame.Size.H)
	for y := 0; y < compiled.Frame.Size.H; y++ {
		rows = append(rows, l.Term.RowText(y))
	}
	return Snapshot{
		VisibleLines: compiler.VisibleLines(compiled.LayoutTree, l.App.View(l.State)),
		FocusGraph:   compiled.FocusGraph,
		FocusMemory:  l.Focus,
		FrameSize:    compiled.Frame.Size,
		FrameRows:    rows,
	}
}
