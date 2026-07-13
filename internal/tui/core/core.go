package core

import "context"

// ── Key ───────────────────────────────────────────────────────────────

// Key is a semantic identity for a node. Keys are stable across re-renders
// and are used for focus, routing, and diagnostics.
type Key string

func (k Key) Empty() bool           { return k == "" }
func (k Key) Child(name string) Key { return Key(string(k) + "." + name) }

// ── Signal ────────────────────────────────────────────────────────────

// SignalKind classifies how a user intent is translated into a typed event.
type SignalKind string

const (
	SignalActivate SignalKind = "activate"
	SignalChange   SignalKind = "change"
	SignalCommit   SignalKind = "commit"
	SignalCancel   SignalKind = "cancel"
	SignalFocus    SignalKind = "focus"
)

// Signal connects a node interaction to a typed application event.
type Signal[E any] struct {
	Kind  SignalKind
	Event E
}

// Shortcut is a global intent binding on a node.
// Shortcuts are resolved regardless of focus state or disabled status.
type Shortcut[E any] struct {
	Intent Intent
	Signal Signal[E]
}

// newSignal is an unexported helper used by constructors.
func newSignal[E any](kind SignalKind, event E) Signal[E] {
	return Signal[E]{Kind: kind, Event: event}
}

// ── Role / VisualState ───────────────────────────────────────────────

// Role is a semantic role used for accessibility, theming, and testing.
type Role string

const (
	RoleNone     Role = ""
	RoleRoot     Role = "root"
	RolePanel    Role = "panel"
	RoleHeading  Role = "heading"
	RoleLabel    Role = "label"
	RoleValue    Role = "value"
	RoleMuted    Role = "muted"
	RoleDanger   Role = "danger"
	RoleSuccess  Role = "success"
	RoleWarning  Role = "warning"
	RoleFocus    Role = "focus"
	RoleSelected Role = "selected"
	RoleDisabled Role = "disabled"
)

// knownRoles is the set of supported roles for validation.
var knownRoles = map[Role]struct{}{
	RoleNone: {}, RoleRoot: {}, RolePanel: {}, RoleHeading: {},
	RoleLabel: {}, RoleValue: {}, RoleMuted: {}, RoleDanger: {},
	RoleSuccess: {}, RoleWarning: {}, RoleFocus: {}, RoleSelected: {},
	RoleDisabled: {},
}

// VisualState is a bitmask of visual states applied to a node.
type VisualState uint16

const (
	StateNormal  VisualState = 0
	StateFocused VisualState = 1 << iota
	StateSelected
	StateDisabled
	StateActive
	StateError
	StateStale
	StateLoading
)

// ── Layout primitives ────────────────────────────────────────────────

// Size is a width/height pair in terminal cells.
type Size struct{ W, H int }

func (s Size) Zero() bool { return s.W == 0 && s.H == 0 }

// Axis determines the primary layout direction.
type Axis int

const (
	AxisVertical Axis = iota
	AxisHorizontal
)

// Sizing determines how a length is resolved during layout.
type Sizing int

const (
	SizeFit Sizing = iota
	SizeFixed
	SizeFill
)

// Length is a constraint on one dimension.
type Length struct {
	Kind  Sizing
	Value int // cell count for Fixed, weight for Fill
	Min   int
	Max   int // 0 means unbounded
}

func (l Length) valid() bool {
	if l.Max > 0 && l.Max < l.Min {
		return false
	}
	if l.Kind == SizeFixed && l.Value < 0 {
		return false
	}
	return true
}

// Overflow determines how content that exceeds bounds is handled.
type Overflow int

const (
	OverflowClip Overflow = iota
	OverflowWrap
	OverflowScroll
)

// ── Style primitives (skeleton) ──────────────────────────────────────

// StyleID is a resolved style reference; style resolution lives in compiler.
type StyleID uint32

// ── Focus primitives ─────────────────────────────────────────────────

// FocusID identifies a focusable node.
type FocusID string

// FocusScopeID identifies a named focus boundary.
type FocusScopeID string

// FocusScope declares a named focus boundary.
type FocusScope struct {
	ID     FocusScopeID
	Parent FocusScopeID // empty if root
}

// FocusMemory tracks runtime focus state across re-renders.
type FocusMemory struct {
	ActiveFocusID FocusID
	ScopeFocusID  map[FocusScopeID]FocusID // last focused in each scope
}

func (fm FocusMemory) Empty() bool {
	return fm.ActiveFocusID == "" && len(fm.ScopeFocusID) == 0
}

// ── Intent ───────────────────────────────────────────────────────────

// Intent is a semantic user action, translated from raw input by the runtime.
type Intent string

const (
	IntentMoveNext     Intent = "move-next"
	IntentMovePrevious Intent = "move-previous"
	IntentMoveLeft     Intent = "move-left"
	IntentMoveRight    Intent = "move-right"
	IntentActivate     Intent = "activate"
	IntentCancel       Intent = "cancel"
	IntentEdit         Intent = "edit"
	IntentBackspace    Intent = "backspace"
	IntentPaste        Intent = "paste"
	IntentSwitchTab1   Intent = "switch-tab-1"
	IntentSwitchTab2   Intent = "switch-tab-2"
	IntentNextTab      Intent = "next-tab"
	IntentPrevTab      Intent = "prev-tab"
	IntentHelp         Intent = "help"
	IntentQuit         Intent = "quit"
)

// ── Node[E] ───────────────────────────────────────────────────────────

// Node is immutable semantic UI intent.
// The type parameter E is the product's event type; Action and Field nodes
// carry typed signals.
type Node[E any] struct {
	kind nodeKind

	k   Key
	lbl string // Text label, Field value, or Action description
	ax  Axis
	ch  []Node[E] // children; copied by constructors

	role  Role
	state VisualState

	focusID  FocusID
	scopeID  FocusScopeID
	disabled bool
	expanded bool // Region disclosure state

	shortcuts []Shortcut[E] // global shortcuts; active regardless of focus/disabled

	// Interaction
	accepts []Intent       // which intents this node handles
	signals []Signal[E]    // signals fired on accepted intents
	change  func(string) E // Field: text change → event
	commit  func(string) E // Field: commit value → event
	cancel  func() E       // Field: cancel edit → event

	lenW Length   // width constraint
	lenH Length   // height constraint
	ovf  Overflow // overflow strategy
}

// nodeKind is unexported; exhaustive switch lives in compiler.
type nodeKind int

const (
	kindEmpty nodeKind = iota
	kindText
	kindRegion
	kindFlow
	kindAction
	kindField
)

func (k nodeKind) String() string {
	switch k {
	case kindEmpty:
		return "empty"
	case kindText:
		return "text"
	case kindRegion:
		return "region"
	case kindFlow:
		return "flow"
	case kindAction:
		return "action"
	case kindField:
		return "field"
	default:
		return "unknown"
	}
}

// ── Constructors (zero value is Empty) ───────────────────────────────

// Empty returns the empty node. Zero-value Node is equivalent.
func Empty[E any]() Node[E] { return Node[E]{kind: kindEmpty} }

// Text returns a text node.
func Text[E any](key Key, label string) Node[E] {
	return Node[E]{kind: kindText, k: key, lbl: label}
}

// Region returns a disclosure region containing children.
func Region[E any](key Key, expanded bool, children ...Node[E]) Node[E] {
	return Node[E]{kind: kindRegion, k: key, expanded: expanded, ch: clone(children)}
}

// Flow returns a container with a layout axis and children.
func Flow[E any](key Key, axis Axis, children ...Node[E]) Node[E] {
	return Node[E]{kind: kindFlow, k: key, ax: axis, ch: clone(children)}
}

// Action returns an interactive action node.
func Action[E any](key Key, label string, disabled bool, activate E) Node[E] {
	return Node[E]{
		kind:     kindAction,
		k:        key,
		lbl:      label,
		disabled: disabled,
		accepts:  []Intent{IntentActivate},
		signals:  []Signal[E]{newSignal(SignalActivate, activate)},
	}
}

// Field returns an editable text field node.
func Field[E any](key Key, value string, onChange func(string) E, onCommit func(string) E, onCancel func() E) Node[E] {
	return Node[E]{
		kind:    kindField,
		k:       key,
		lbl:     value,
		accepts: []Intent{IntentEdit, IntentBackspace, IntentActivate, IntentCancel},
		change:  onChange,
		commit:  onCommit,
		cancel:  onCancel,
	}
}

// WithRole attaches a semantic role.
func (n Node[E]) WithRole(r Role) Node[E] {
	n.role = r
	return n
}

// WithVisualState attaches a visual state mask.
func (n Node[E]) WithVisualState(vs VisualState) Node[E] {
	n.state = vs
	return n
}

// WithFocusID overrides the auto-derived FocusID.
func (n Node[E]) WithFocusID(id FocusID) Node[E] {
	n.focusID = id
	return n
}

// WithScopeID places this node inside a named focus scope.
func (n Node[E]) WithScopeID(id FocusScopeID) Node[E] {
	n.scopeID = id
	return n
}

// WithSize sets width/height constraints.
func (n Node[E]) WithSize(w, h Length) Node[E] {
	n.lenW, n.lenH = w, h
	return n
}

// WithOverflow sets overflow strategy.
func (n Node[E]) WithOverflow(o Overflow) Node[E] {
	n.ovf = o
	return n
}

// WithAccepts replaces the accepted intents.
func (n Node[E]) WithAccepts(intents []Intent) Node[E] {
	n.accepts = append([]Intent(nil), intents...)
	return n
}

// WithSignals replaces the emitted signals.
func (n Node[E]) WithSignals(sigs []Signal[E]) Node[E] {
	cp := make([]Signal[E], len(sigs))
	copy(cp, sigs)
	n.signals = cp
	return n
}

// WithShortcut adds a global shortcut intent to this node.
// Shortcuts are active regardless of focus state or disabled status.
func (n Node[E]) WithShortcut(intent Intent, signal Signal[E]) Node[E] {
	n.shortcuts = append(n.shortcuts, Shortcut[E]{Intent: intent, Signal: signal})
	return n
}

// ── Accessors (defensive copies) ─────────────────────────────────────

func (n Node[E]) Key() Key                 { return n.k }
func (n Node[E]) Kind() string             { return n.kind.String() }
func (n Node[E]) Children() []Node[E]      { return clone(n.ch) }
func (n Node[E]) Label() string            { return n.lbl }
func (n Node[E]) Axis() Axis               { return n.ax }
func (n Node[E]) Role() Role               { return n.role }
func (n Node[E]) VisualState() VisualState { return n.state }
func (n Node[E]) FocusID() FocusID         { return n.focusID }
func (n Node[E]) ScopeID() FocusScopeID    { return n.scopeID }
func (n Node[E]) IsDisabled() bool         { return n.disabled }
func (n Node[E]) IsExpanded() bool         { return n.kind == kindRegion && n.expanded }
func (n Node[E]) IsCollapsed() bool        { return n.kind == kindRegion && !n.expanded }
func (n Node[E]) Accepts() []Intent        { return cloneIntents(n.accepts) }
func (n Node[E]) Signals() []Signal[E]     { return cloneSignals(n.signals) }
func (n Node[E]) Shortcuts() []Shortcut[E] {
	if len(n.shortcuts) == 0 {
		return nil
	}
	out := make([]Shortcut[E], len(n.shortcuts))
	copy(out, n.shortcuts)
	return out
}
func (n Node[E]) OnChange() func(string) E { return n.change }
func (n Node[E]) OnCommit() func(string) E { return n.commit }
func (n Node[E]) OnCancel() func() E       { return n.cancel }
func (n Node[E]) Width() Length            { return n.lenW }
func (n Node[E]) Height() Length           { return n.lenH }
func (n Node[E]) Overflow() Overflow       { return n.ovf }

// IsRunnable reports whether the node participates in focus-scoped interaction.
// A node is runnable when it is not disabled and accepts at least one intent.
func (n Node[E]) IsRunnable() bool {
	return !n.disabled && len(n.accepts) > 0
}

func clone[E any](in []Node[E]) []Node[E] {
	if len(in) == 0 {
		return nil
	}
	out := make([]Node[E], len(in))
	copy(out, in)
	return out
}

func cloneIntents(in []Intent) []Intent {
	if len(in) == 0 {
		return nil
	}
	out := make([]Intent, len(in))
	copy(out, in)
	return out
}

func cloneSignals[E any](in []Signal[E]) []Signal[E] {
	if len(in) == 0 {
		return nil
	}
	out := make([]Signal[E], len(in))
	copy(out, in)
	return out
}

// ── Effect ───────────────────────────────────────────────────────────

// EffectPolicy determines how overlapping effects are handled.
type EffectPolicy int

const (
	EffectRun EffectPolicy = iota
	EffectCancelPrevious
	EffectIgnoreWhileRunning
)

// EffectKey identifies an effect for cancellation policy.
type EffectKey string

// Effect declares external async work.
type Effect[E any] struct {
	Key    EffectKey
	Policy EffectPolicy
	Run    func(context.Context) E
}

// EmptyEffect returns a do-nothing effect (used for nil-safety).
func EmptyEffect[E any]() Effect[E] {
	var zero E
	return Effect[E]{Key: "", Policy: EffectRun, Run: func(context.Context) E { return zero }}
}

// ── App contract ─────────────────────────────────────────────────────

// App[S,E] is the product application contract.
type App[S any, E any] interface {
	Init() (S, []Effect[E])
	Update(state S, event E) (S, []Effect[E])
	View(state S) Node[E]
}

// ── Frame output ─────────────────────────────────────────────────────

// Cell is one terminal cell.
type Cell struct {
	Rune  rune
	Style StyleID
}

// Frame is the resolved terminal cell buffer.
type Frame struct {
	Size   Size
	Cells  []Cell
	Cursor Cursor
}

// Rect is an axis-aligned rectangle in terminal cells.
type Rect struct{ X, Y, W, H int }

func (r Rect) Empty() bool { return r.W == 0 || r.H == 0 }

// Cursor position in terminal cells (zero-based).
type Cursor struct {
	X, Y    int
	Visible bool
}

// ── Diagnostics ──────────────────────────────────────────────────────

// DiagnosticSeverity classifies the importance of a diagnostic.
type DiagnosticSeverity int

const (
	SeverityError DiagnosticSeverity = iota
	SeverityWarning
)

// Diagnostic records a validation or compilation issue.
type Diagnostic struct {
	Severity DiagnosticSeverity
	Message  string
	Key      Key
	Path     []Key  // key path from root to the offending node
	Hint     string // actionable guidance for fixing the issue
}

// EqualReports whether two diagnostics are equal for deterministic output comparison.
func (d Diagnostic) EqualReports(other Diagnostic) bool {
	if d.Severity != other.Severity || d.Message != other.Message || d.Key != other.Key || d.Hint != other.Hint {
		return false
	}
	if len(d.Path) != len(other.Path) {
		return false
	}
	for i := range d.Path {
		if d.Path[i] != other.Path[i] {
			return false
		}
	}
	return true
}

// Diagnostics is an aggregate of compilation messages.
type Diagnostics struct {
	Items []Diagnostic
}

// Add appends a diagnostic.
func (d *Diagnostics) Add(sev DiagnosticSeverity, msg string, key Key, hint string) {
	d.Items = append(d.Items, Diagnostic{Severity: sev, Message: msg, Key: key, Hint: hint})
}

// HasErrors returns true if any item is an error.
func (d Diagnostics) HasErrors() bool {
	for _, item := range d.Items {
		if item.Severity == SeverityError {
			return true
		}
	}
	return false
}

// HasWarnings returns true if any item is a warning.
func (d Diagnostics) HasWarnings() bool {
	for _, item := range d.Items {
		if item.Severity == SeverityWarning {
			return true
		}
	}
	return false
}

// ErrCount returns the number of error-level diagnostics.
func (d Diagnostics) ErrCount() int {
	var n int
	for _, item := range d.Items {
		if item.Severity == SeverityError {
			n++
		}
	}
	return n
}

// WarnCount returns the number of warning-level diagnostics.
func (d Diagnostics) WarnCount() int {
	var n int
	for _, item := range d.Items {
		if item.Severity == SeverityWarning {
			n++
		}
	}
	return n
}

// ── Validate ─────────────────────────────────────────────────────────

// Validator checks a semantic tree for structural and semantic invariants.
type Validator[E any] struct {
	Diags Diagnostics
	seen  map[Key]struct{}
}

// Validate checks a Node[E] tree for deterministic structural errors.
// Validation is semantic only; it does not need layout, style, or focus output.
func Validate[E any](root Node[E]) Diagnostics {
	v := Validator[E]{seen: map[Key]struct{}{}}
	v.walk(root, nil)
	return v.Diags
}

func (v *Validator[E]) walk(n Node[E], path []Key) {
	if n.kind == kindEmpty {
		return
	}
	path = append(path, n.Key())
	k := n.Key()

	// ── Missing key ──
	if k.Empty() {
		v.Diags.Add(
			SeverityError,
			"non-empty node has empty key",
			Key(""),
			"assign a stable Key to every non-empty node for identity, focus, and routing",
		)
		// Duplicate-key tracking is skipped for empty keys.
	} else {
		if _, ok := v.seen[k]; ok {
			v.Diags.Add(
				SeverityError,
				"duplicate key: "+string(k),
				k,
				"ensure keys are unique across the entire tree",
			)
		}
		v.seen[k] = struct{}{}
	}

	// ── Role validation ──
	if _, ok := knownRoles[n.Role()]; !ok {
		v.Diags.Add(
			SeverityWarning,
			"unsupported role: "+string(n.Role()),
			k,
			"use a known core.Role constant",
		)
	}

	// ── Length constraint validation ──
	if !n.Width().valid() {
		v.Diags.Add(
			SeverityWarning,
			"invalid width constraint",
			k,
			"Length: Max must not be less than Min, and Fixed values must be non-negative",
		)
	}
	if !n.Height().valid() {
		v.Diags.Add(
			SeverityWarning,
			"invalid height constraint",
			k,
			"Length: Max must not be less than Min, and Fixed values must be non-negative",
		)
	}

	// ── Focusability validation ──
	hasAccepts := len(n.Accepts()) > 0
	hasFocusID := n.FocusID() != ""
	if hasAccepts && !hasFocusID {
		v.Diags.Add(
			SeverityWarning,
			"focusable node without FocusID",
			k,
			"assign a FocusID or WithFocusID so the runtime can route intents",
		)
	}
	if !hasAccepts && hasFocusID {
		v.Diags.Add(
			SeverityWarning,
			"node with FocusID accepts no intents",
			k,
			"remove FocusID or add accepted intents so the node participates in interaction",
		)
	}

	// ── Kind-specific invariants ──
	switch n.kind {
	case kindAction:
		if len(n.Signals()) == 0 {
			v.Diags.Add(
				SeverityError,
				"action node has no signals",
				k,
				"action must emit at least one signal (typically activate)",
			)
		}
	case kindField:
		if n.OnChange() == nil {
			v.Diags.Add(
				SeverityError,
				"field node has nil OnChange callback",
				k,
				"field requires change, commit, and cancel callbacks",
			)
		}
		if n.OnCommit() == nil {
			v.Diags.Add(
				SeverityError,
				"field node has nil OnCommit callback",
				k,
				"field requires change, commit, and cancel callbacks",
			)
		}
		if n.OnCancel() == nil {
			v.Diags.Add(
				SeverityError,
				"field node has nil OnCancel callback",
				k,
				"field requires change, commit, and cancel callbacks",
			)
		}
	}

	// ── Disabled interaction rules ──
	if n.IsDisabled() {
		if len(n.Signals()) > 0 {
			v.Diags.Add(
				SeverityWarning,
				"disabled node declares signals",
				k,
				"signals on a disabled node are unreachable; remove signals or enable the node",
			)
		}
		if len(n.Accepts()) > 0 {
			v.Diags.Add(
				SeverityWarning,
				"disabled node declares accepted intents",
				k,
				"intents on a disabled node are unreachable; remove accepts or enable the node",
			)
		}
	}

	// Recursive descent; copy path for each child to preserve per-branch paths.
	for _, ch := range n.Children() {
		cp := make([]Key, len(path))
		copy(cp, path)
		v.walk(ch, cp)
	}
}
