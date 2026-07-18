package ui

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
)

// SelectProps configures a Select.
type SelectProps struct {
	ID        string
	Label     string
	Value     string // committed value shown in the row
	Action    string // optional row action; defaults from state if empty
	Detail    string // optional committed-value detail shown only while closed
	AutoFocus bool

	// CanEnter determines if the select can be entered. If nil, always allows entry.
	CanEnter func() bool

	// OnEnter is called when the select is entered.
	OnEnter func()

	// OnBackout is called when the select backs out.
	OnBackout func()

	// Body returns the entered content (a SearchPicker, a list of rows, an
	// input, …). It receives the Select's backout func so its own cancel /
	// escape can close the select.
	Body func(backout func()) tui.Component
}

// Select is a self-managing row that owns its own entered state: it renders a
// SelectableRow and, when entered, the Body beneath it. Entered state lives
// here — callers compose selects without a shared "which is open" enum.
type Select struct {
	props   SelectProps
	entered *tui.State[bool]
}

func NewSelect(props SelectProps) *Select {
	return &Select{props: props, entered: tui.NewState(false)}
}

func (s *Select) Init() func() { return nil }

func (s *Select) BindApp(app *tui.App) {
	if s.entered != nil {
		s.entered.BindApp(app)
	}
}

func (s *Select) UnbindApp() {}

func (s *Select) UpdateProps(fresh tui.Component) {
	if f, ok := fresh.(*Select); ok {
		s.props = f.props
	}
}

// Render is generated into select_gsx.go from select.gsx. Visual structure
// lives in the GSX template; behavior stays here in the helpers below.

func (s *Select) headerRow() *SelectableRow {
	row := NewSelectableRow(s.props.ID, s.props.Label, s.props.Value, s.actionLabel(), s.activate)
	// Only own Escape while entered; when not entered, Escape bubbles to the
	// caller's back navigation.
	if s.entered.Get() {
		row.OnEscape = s.Backout
	}
	row.AutoFocus = s.props.AutoFocus
	return row
}

// SelectHeaderComponent returns the Select's header row. The GSX framework
// mounts it so it keeps stable identity across renders. Visual structure lives
// in select.gsx; this is the behavior-only constructor behind the @-component.
func SelectHeaderComponent(s *Select) *SelectableRow {
	return s.headerRow()
}

// SelectBodyComponent returns the entered body, seeded with the Select's
// backout func so bodies do not invent their own close semantics.
func SelectBodyComponent(s *Select) tui.Component {
	return s.props.Body(s.Backout)
}

func (s *Select) actionLabel() string {
	if s.entered.Get() {
		return "close ↵"
	}
	if s.props.Action != "" {
		return s.props.Action
	}
	if strings.TrimSpace(s.props.Value) == "" {
		return "choose ↵"
	}
	return "change ↵"
}

// activate runs the primary action. If entered, it backs out. If not entered,
// it attempts to enter.
func (s *Select) activate() {
	if s.entered.Get() {
		s.Backout()
		return
	}
	s.Enter()
}

// Enter attempts to enter the select. If CanEnter is defined and returns false,
// entry is refused. Otherwise, entered state is set to true and OnEnter is called.
// OnEnter is only called when transitioning from not-entered to entered state.
func (s *Select) Enter() {
	if s.entered.Get() {
		// Already entered, no-op
		return
	}

	if s.props.CanEnter != nil && !s.props.CanEnter() {
		return
	}

	s.entered.Set(true)

	if s.props.OnEnter != nil {
		s.props.OnEnter()
	}
}

// Backout exits the select. If entered, it sets entered to false and calls
// OnBackout if defined. This is a no-op if not entered. Exported because
// "backout" is a public grammar verb; feature packages may drive a Select's
// backout from feature-level lifecycle code (e.g. Escape fallbacks).
func (s *Select) Backout() {
	if !s.entered.Get() {
		return
	}

	s.entered.Set(false)

	if s.props.OnBackout != nil {
		s.props.OnBackout()
	}
}

// IsEntered returns whether the select is currently in entered state.
func (s *Select) IsEntered() bool {
	return s.entered.Get()
}

var (
	_ tui.Component    = (*Select)(nil)
	_ tui.Initializer  = (*Select)(nil)
	_ tui.AppBinder    = (*Select)(nil)
	_ tui.PropsUpdater = (*Select)(nil)
)
