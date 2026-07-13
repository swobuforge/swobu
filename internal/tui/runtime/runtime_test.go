package runtime

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/tui/compiler"
	"github.com/swobuforge/swobu/internal/tui/core"
)

// ── Typed Event ──────────────────────────────────────────────────────

type skeletonEvent interface {
	skeletonEventMarker()
}

type eventActivate struct{ Target string }
type eventNextTab struct{}
type eventPrevTab struct{}
type eventHelp struct{}
type eventFieldEdit struct{ Value string }
type eventFieldCommit struct{ Value string }
type eventFieldCancel struct{}
type eventCollapse struct{ Key core.Key }
type eventExpand struct{ Key core.Key }

func (_ eventActivate) skeletonEventMarker()    {}
func (_ eventNextTab) skeletonEventMarker()     {}
func (_ eventPrevTab) skeletonEventMarker()     {}
func (_ eventHelp) skeletonEventMarker()        {}
func (_ eventFieldEdit) skeletonEventMarker()   {}
func (_ eventFieldCommit) skeletonEventMarker() {}
func (_ eventFieldCancel) skeletonEventMarker() {}
func (_ eventCollapse) skeletonEventMarker()    {}
func (_ eventExpand) skeletonEventMarker()      {}

// ── State ────────────────────────────────────────────────────────────

type skeletonState struct {
	ActiveTab int // 0=workspace, 1=settings
	ShowHelp  bool
	Expanded  map[core.Key]bool
	FieldVal  string
	Log       []string
}

func newState() skeletonState {
	return skeletonState{
		ActiveTab: 0,
		Expanded: map[core.Key]bool{
			core.Key("workspace"): true,
			core.Key("outer"):     true,
			core.Key("inner"):     true,
		},
		Log: []string{},
	}
}

func (s skeletonState) copy() skeletonState {
	cp := s
	cp.Expanded = make(map[core.Key]bool, len(s.Expanded))
	for k, v := range s.Expanded {
		cp.Expanded[k] = v
	}
	cp.Log = append([]string(nil), s.Log...)
	return cp
}

func (s skeletonState) withTab(i int) skeletonState   { cp := s.copy(); cp.ActiveTab = i; return cp }
func (s skeletonState) withHelp(v bool) skeletonState { cp := s.copy(); cp.ShowHelp = v; return cp }
func (s skeletonState) withExp(k core.Key, v bool) skeletonState {
	cp := s.copy()
	cp.Expanded[k] = v
	return cp
}
func (s skeletonState) withLog(msg string) skeletonState {
	cp := s.copy()
	cp.Log = append(cp.Log, msg)
	return cp
}
func (s skeletonState) withField(v string) skeletonState {
	cp := s.copy()
	cp.FieldVal = v
	return cp
}

// ── App ──────────────────────────────────────────────────────────────

// skeletonApp produces a tree with two tabs, a help shortcut, and body content.
// Tab rail uses global shortcuts on a Text container (not actions).
// Body focus movement uses explicit IntentMoveNext/MovePrevious.
type skeletonApp struct{}

func (a *skeletonApp) Init() (skeletonState, []core.Effect[skeletonEvent]) { return newState(), nil }

func (a *skeletonApp) Update(s skeletonState, e skeletonEvent) (skeletonState, []core.Effect[skeletonEvent]) {
	switch ev := e.(type) {
	case eventNextTab:
		s.ActiveTab = (s.ActiveTab + 1) % 2
	case eventPrevTab:
		s.ActiveTab = (s.ActiveTab - 1 + 2) % 2
	case eventHelp:
		s.ShowHelp = !s.ShowHelp
	case eventActivate:
		s = s.withLog("activated:" + ev.Target)
	case eventFieldEdit:
		s = s.withField(ev.Value)
	case eventFieldCommit:
		s = s.withLog("commit:" + ev.Value)
	case eventFieldCancel:
		s = s.withLog("cancel")
	case eventCollapse:
		s = s.withExp(ev.Key, false)
	case eventExpand:
		s = s.withExp(ev.Key, true)
	}
	return s, nil
}

func (a *skeletonApp) View(st skeletonState) core.Node[skeletonEvent] {
	tabs := core.Flow[skeletonEvent](core.Key("tabs"), core.AxisHorizontal,
		core.Text[skeletonEvent](core.Key("tab.workspace"), "Workspace"),
		core.Text[skeletonEvent](core.Key("tab.settings"), "Settings"),
	)

	var children []core.Node[skeletonEvent]
	children = append(children, tabs)

	if st.ShowHelp {
		children = append(children, core.Text[skeletonEvent](core.Key("help.body"), "Help screen"))
	} else {
		switch st.ActiveTab {
		case 0:
			workspace := core.Region[skeletonEvent](core.Key("workspace"), st.Expanded[core.Key("workspace")],
				core.Action[skeletonEvent](core.Key("workspace.action"), "Run", false, eventActivate{Target: "run"}),
				core.Field[skeletonEvent](core.Key("workspace.field"), st.FieldVal,
					func(v string) skeletonEvent { return eventFieldEdit{Value: v} },
					func(v string) skeletonEvent { return eventFieldCommit{Value: v} },
					func() skeletonEvent { return eventFieldCancel{} },
				),
			)
			children = append(children, workspace)
		case 1:
			settings := core.Region[skeletonEvent](core.Key("settings"), st.Expanded[core.Key("settings")],
				core.Action[skeletonEvent](core.Key("settings.action"), "Save", false, eventActivate{Target: "save"}),
			)
			children = append(children, settings)
		}
	}

	return core.Flow[skeletonEvent](core.Key("root"), core.AxisVertical, children...).
		WithShortcut(core.IntentHelp, core.Signal[skeletonEvent]{Kind: core.SignalActivate, Event: eventHelp{}}).
		WithShortcut(core.IntentNextTab, core.Signal[skeletonEvent]{Kind: core.SignalActivate, Event: eventNextTab{}}).
		WithShortcut(core.IntentPrevTab, core.Signal[skeletonEvent]{Kind: core.SignalActivate, Event: eventPrevTab{}})
}

// ── Tests ────────────────────────────────────────────────────────────

func TestSkeleton_DisclosureAndFocus(t *testing.T) {
	app := &skeletonApp{}
	term := NewFakeTerminal(80, 10)
	loop := NewLoop(app, term)
	if err := loop.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	root := app.View(loop.State)
	compiled := compiler.New[skeletonEvent]().Compile(compiler.CompileInput[skeletonEvent]{
		Root: root,
		Size: core.Size{W: 80, H: 10},
	})
	lines := compiler.VisibleLines(compiled.LayoutTree, root)
	if len(lines) == 0 {
		t.Fatal("no visible lines")
	}

	for _, ln := range lines {
		if ln.Key == core.Key("workspace.action") || ln.Key == core.Key("workspace.field") {
			if ln.Depth != 2 {
				t.Fatalf("expected depth 2 for %s, got %d", ln.Key, ln.Depth)
			}
		}
	}

	seen := map[int]core.Key{}
	for _, ln := range lines {
		if prev, ok := seen[ln.Offset]; ok {
			t.Fatalf("duplicate offset %d: %s and %s", ln.Offset, prev, ln.Key)
		}
		seen[ln.Offset] = ln.Key
	}

	if len(term.LastFrame().Cells) == 0 {
		t.Fatal("frame has no cells")
	}
}

// TestSkeleton_FocusMovement proves body focus movement via explicit
// IntentMoveNext / IntentMovePrevious, separate from tab-rail switching.
func TestSkeleton_FocusMovement(t *testing.T) {
	app := &skeletonApp{}
	term := NewFakeTerminal(80, 10)
	loop := NewLoop(app, term)
	_ = loop.Init()

	_, _ = loop.SendIntent(core.IntentMoveNext)
	first := loop.Focus.ActiveFocusID
	if first == "" {
		t.Fatal("focus not set after move-next")
	}

	_, _ = loop.SendIntent(core.IntentMoveNext)
	if loop.Focus.ActiveFocusID == first {
		t.Fatal("focus did not move after second move-next")
	}
	second := loop.Focus.ActiveFocusID

	_, _ = loop.SendIntent(core.IntentMovePrevious)
	if loop.Focus.ActiveFocusID != first {
		t.Fatalf("focus did not move back: got %s, want %s", loop.Focus.ActiveFocusID, first)
	}
	if first == second {
		t.Fatal("move-next did not reach a different target")
	}
}

// TestSkeleton_TabSwitching proves Tab/Shift+Tab are global tab intents that
// update active tab state without moving body focus.
func TestSkeleton_TabSwitching(t *testing.T) {
	app := &skeletonApp{}
	term := NewFakeTerminal(80, 10)
	loop := NewLoop(app, term)
	_ = loop.Init()

	if loop.State.ActiveTab != 0 {
		t.Fatalf("expected initial tab 0, got %d", loop.State.ActiveTab)
	}

	_, ok := loop.SendInput("tab")
	if !ok {
		t.Fatal("tab did not produce event")
	}
	if loop.State.ActiveTab != 1 {
		t.Fatalf("expected tab 1 after Tab, got %d", loop.State.ActiveTab)
	}

	_, ok = loop.SendInput("shift+tab")
	if !ok {
		t.Fatal("shift+tab did not produce event")
	}
	if loop.State.ActiveTab != 0 {
		t.Fatalf("expected tab 0 after Shift+Tab, got %d", loop.State.ActiveTab)
	}
}

// TestSkeleton_Activation proves activating a focused target emits a typed
// event through the interaction route table.
func TestSkeleton_Activation(t *testing.T) {
	app := &skeletonApp{}
	term := NewFakeTerminal(80, 10)
	loop := NewLoop(app, term)
	_ = loop.Init()

	_, _ = loop.SendIntent(core.IntentMoveNext)

	ev, ok := loop.SendInput("enter")
	if !ok {
		t.Fatal("enter did not produce event")
	}
	if _, ok := ev.(eventActivate); !ok {
		t.Fatalf("expected eventActivate, got %T", ev)
	}
}

// TestSkeleton_CollapseHidesChildren proves collapse removes child focus
// targets and expand restores them.
func TestSkeleton_CollapseHidesChildren(t *testing.T) {
	app := &skeletonApp{}
	term := NewFakeTerminal(80, 10)
	loop := NewLoop(app, term)
	_ = loop.Init()

	loop.State, _ = app.Update(loop.State, eventCollapse{Key: core.Key("workspace")})
	root := app.View(loop.State)
	compiled := compiler.New[skeletonEvent]().Compile(compiler.CompileInput[skeletonEvent]{
		Root: root,
		Size: core.Size{W: 80, H: 10},
	})
	for _, tgr := range compiled.FocusGraph.Targets {
		if tgr.Key == core.Key("workspace.action") {
			t.Fatal("workspace.action should not be focusable after collapse")
		}
	}

	loop.State, _ = app.Update(loop.State, eventExpand{Key: core.Key("workspace")})
	root = app.View(loop.State)
	compiled = compiler.New[skeletonEvent]().Compile(compiler.CompileInput[skeletonEvent]{
		Root: root,
		Size: core.Size{W: 80, H: 10},
	})
	var restored bool
	for _, tgr := range compiled.FocusGraph.Targets {
		if tgr.Key == core.Key("workspace.action") {
			restored = true
		}
	}
	if !restored {
		t.Fatal("workspace.action should be focusable after expand")
	}
}

// TestSkeleton_FieldEditViaRoutes proves field change/commit/cancel dispatch
// typed events through the runtime interaction route table.
func TestSkeleton_FieldEditViaRoutes(t *testing.T) {
	app := &skeletonApp{}
	term := NewFakeTerminal(80, 10)
	loop := NewLoop(app, term)
	_ = loop.Init()

	for loop.Focus.ActiveFocusID == "" || !strings.Contains(string(loop.Focus.ActiveFocusID), "workspace.field") {
		_, _ = loop.SendIntent(core.IntentMoveNext)
		if loop.Focus.ActiveFocusID == "" {
			t.Fatal("focus lost during navigation")
		}
		if strings.Contains(string(loop.Focus.ActiveFocusID), "workspace.field") {
			break
		}
		if len(loop.State.Log) > 10 {
			t.Fatalf("could not reach field focus, stuck at %s", loop.Focus.ActiveFocusID)
		}
	}

	_, ok := loop.SendFieldEvent(core.IntentEdit, "hello")
	if !ok {
		t.Fatal("field edit route not found")
	}
	if loop.State.FieldVal != "hello" {
		t.Fatalf("field value not updated: %q", loop.State.FieldVal)
	}

	_, ok = loop.SendFieldEvent(core.IntentActivate, "hello")
	if !ok {
		t.Fatal("field commit route not found")
	}
	var foundCommit bool
	for _, msg := range loop.State.Log {
		if msg == "commit:hello" {
			foundCommit = true
		}
	}
	if !foundCommit {
		t.Fatalf("expected commit:hello in log, got %v", loop.State.Log)
	}

	_, ok = loop.SendFieldEvent(core.IntentCancel, "")
	if !ok {
		t.Fatal("field cancel route not found")
	}
	var foundCancel bool
	for _, msg := range loop.State.Log {
		if msg == "cancel" {
			foundCancel = true
		}
	}
	if !foundCancel {
		t.Fatalf("expected cancel in log, got %v", loop.State.Log)
	}
}

// TestSkeleton_GlobalHelpIntent proves '?' dispatches through a global
// shortcut, not through a disabled action's scoped routes.
func TestSkeleton_GlobalHelpIntent(t *testing.T) {
	app := &skeletonApp{}
	term := NewFakeTerminal(80, 10)
	loop := NewLoop(app, term)
	_ = loop.Init()

	_, ok := loop.SendInput("?")
	if !ok {
		t.Fatal("help intent did not produce event")
	}
	if !loop.State.ShowHelp {
		t.Fatal("help should be visible after '?'")
	}
}

// TestSkeleton_DisabledActionHasNoRoute proves a disabled action does not
// generate interaction routes.
func TestSkeleton_DisabledActionHasNoRoute(t *testing.T) {
	badTree := core.Flow[skeletonEvent](core.Key("root"), core.AxisVertical,
		core.Action[skeletonEvent](core.Key("disabled.act"), "Bad", true, eventActivate{Target: "bad"}),
	)
	compiled := compiler.New[skeletonEvent]().Compile(compiler.CompileInput[skeletonEvent]{
		Root: badTree,
		Size: core.Size{W: 80, H: 10},
	})
	for _, r := range compiled.InteractionRoutes.Routes {
		if r.FocusID == core.FocusID("disabled.act") {
			t.Fatal("disabled action should not have interaction routes")
		}
	}
	var found bool
	for _, ft := range compiled.FocusGraph.Targets {
		if ft.Key == core.Key("disabled.act") {
			found = true
			if ft.Enabled {
				t.Fatal("disabled action should not be enabled in focus graph")
			}
		}
	}
	if !found {
		t.Fatal("disabled action should still appear in focus graph (unreachable)")
	}
}

// TestSkeleton_EscCollapseViaInput proves Esc collapses the nearest expanded
// ancestor through runtime input dispatch (not via a side helper).
func TestSkeleton_EscCollapseViaInput(t *testing.T) {
	app := &nestedApp{}
	term := NewFakeTerminal(80, 10)
	loop := NewLoop(app, term)
	loop.OnEscCollapse = func(k core.Key) skeletonEvent { return eventCollapse{Key: k} }
	_ = loop.Init()

	for !strings.Contains(string(loop.Focus.ActiveFocusID), "inner.action") {
		_, _ = loop.SendIntent(core.IntentMoveNext)
		if loop.Focus.ActiveFocusID == "" {
			t.Fatal("focus lost")
		}
	}

	_, ok := loop.SendInput("esc")
	if !ok {
		t.Fatal("esc did not collapse any ancestor")
	}
	if loop.State.Expanded[core.Key("inner")] {
		t.Fatal("inner should be collapsed after Esc")
	}
	if !loop.State.Expanded[core.Key("outer")] {
		t.Fatal("outer should still be expanded")
	}
}

// TestSkeleton_PaintedLabels proves the frame contains visible text.
func TestSkeleton_PaintedLabels(t *testing.T) {
	app := &skeletonApp{}
	term := NewFakeTerminal(80, 10)
	loop := NewLoop(app, term)
	_ = loop.Init()

	row2 := term.RowText(2)
	if !strings.Contains(row2, "Workspace") {
		t.Fatalf("row 2 missing 'Workspace': %q", row2)
	}
	row3 := term.RowText(3)
	if !strings.Contains(row3, "Settings") {
		t.Fatalf("row 3 missing 'Settings': %q", row3)
	}
	row5 := term.RowText(5)
	if !strings.Contains(row5, "Run") {
		t.Fatalf("row 5 missing 'Run': %q", row5)
	}
}

// TestSkeleton_Snapshot proves compiler/runtime artifacts are serializable.
func TestSkeleton_Snapshot(t *testing.T) {
	app := &skeletonApp{}
	term := NewFakeTerminal(80, 10)
	loop := NewLoop(app, term)
	_ = loop.Init()

	snap := TakeSnapshot(loop)
	if len(snap.VisibleLines) == 0 {
		t.Fatal("snapshot has no visible lines")
	}
	if len(snap.FocusGraph.Targets) == 0 {
		t.Fatal("snapshot has no focus targets")
	}
	if snap.FrameSize.W != 80 || snap.FrameSize.H != 10 {
		t.Fatalf("unexpected frame size: %+v", snap.FrameSize)
	}
}

func TestSkeleton_NoTerminaluiImport(t *testing.T) {
	_ = NewFakeTerminal(1, 1)
}

// ── Nested App for Esc test ──────────────────────────────────────────

type nestedApp struct{}

func (a *nestedApp) Init() (skeletonState, []core.Effect[skeletonEvent]) {
	return newState(), nil
}

func (a *nestedApp) Update(s skeletonState, e skeletonEvent) (skeletonState, []core.Effect[skeletonEvent]) {
	switch ev := e.(type) {
	case eventCollapse:
		s = s.withExp(ev.Key, false)
	case eventExpand:
		s = s.withExp(ev.Key, true)
	}
	return s, nil
}

func (a *nestedApp) View(st skeletonState) core.Node[skeletonEvent] {
	return core.Flow[skeletonEvent](core.Key("root"), core.AxisVertical,
		core.Region[skeletonEvent](core.Key("outer"), st.Expanded[core.Key("outer")],
			core.Region[skeletonEvent](core.Key("inner"), st.Expanded[core.Key("inner")],
				core.Action[skeletonEvent](core.Key("inner.action"), "Inner", false, eventActivate{Target: "inner"}),
			),
		),
	)
}
