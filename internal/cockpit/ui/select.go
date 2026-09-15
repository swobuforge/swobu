package ui

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
)

// SelectProps configures a Select.
type SelectProps struct {
	ID                 string
	Label              string
	Value              string // committed value shown in the row
	Action             string // optional row action; defaults from state if empty
	ValueTone          Tone   // semantic rendition of the committed value
	Detail             string // optional committed-value detail shown only while closed
	AutoFocus          bool
	ReserveLabelColumn bool
	LabelTone          Tone
	FocusValue         bool
	BackoutAction      string
	BodyFlush          bool

	// Entered makes Select externally controlled when non-nil. The caller owns
	// which control is open; Select retains only shell identity and focus handoff.
	Entered func() bool

	// OnActivate replaces entry when the closed control represents an immediate
	// action instead of a choice. Keeping that action on Select lets one mounted
	// control project idle, pending, and completed phases without changing its
	// concrete component identity.
	OnActivate func()

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

// Select owns one stable selectable shell and its entered body. It may own
// entered state itself or project caller-owned entered state when Entered is
// supplied; focus restoration remains local in both modes.
type Select struct {
	props            SelectProps
	entered          *tui.State[bool]
	selectShellNext  bool
	projectedEntered bool
}

func NewSelect(props SelectProps) *Select {
	s := &Select{props: props, entered: tui.NewState(false)}
	s.projectedEntered = s.IsEntered()
	return s
}

func (s *Select) Init() func() { return nil }

func (s *Select) BindApp(app *tui.App) {
	if s.entered != nil {
		s.entered.BindApp(app)
	}
}

func (s *Select) UnbindApp() {}

func (s *Select) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*Select)
	if !ok {
		return
	}
	wasEntered := s.projectedEntered
	s.props = f.props
	s.projectedEntered = s.IsEntered()
	if wasEntered && !s.projectedEntered {
		s.selectShellNext = true
	}
}

// Render is generated into select_gsx.go from select.gsx. Visual structure
// lives in the GSX template; behavior stays here in the helpers below.

func (s *Select) headerRow() *SelectableRow {
	row := NewSelectableRow(s.props.ID, s.props.Label, s.props.Value, s.actionLabel(), s.activate)
	row.ValueTone = s.props.ValueTone
	row.ReserveLabelColumn = s.props.ReserveLabelColumn
	row.LabelTone = s.props.LabelTone
	row.FocusValue = s.props.FocusValue
	// Only own Escape while entered; when not entered, Escape bubbles to the
	// caller's back navigation.
	if s.IsEntered() {
		row.OnEscape = s.Backout
	}
	row.AutoFocus = s.props.AutoFocus || s.selectShellNext
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
	if s.IsEntered() {
		if s.props.BackoutAction != "" {
			return s.props.BackoutAction
		}
		return "close ↵"
	}
	if s.props.OnActivate != nil {
		return s.props.Action
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
	if s.IsEntered() {
		s.Backout()
		return
	}
	if s.props.OnActivate != nil {
		s.props.OnActivate()
		return
	}
	s.Enter()
}

// Enter attempts to enter the select. If CanEnter is defined and returns false,
// entry is refused. Otherwise, entered state is set to true and OnEnter is called.
// OnEnter is only called when transitioning from not-entered to entered state.
func (s *Select) Enter() {
	if s.IsEntered() {
		// Already entered, no-op
		return
	}

	if s.props.CanEnter != nil && !s.props.CanEnter() {
		return
	}

	s.selectShellNext = false
	if s.props.Entered == nil {
		s.entered.Set(true)
	}

	if s.props.OnEnter != nil {
		s.props.OnEnter()
	}
	s.projectedEntered = s.IsEntered()
}

// Backout exits the select. If entered, it sets entered to false and calls
// OnBackout if defined. This is a no-op if not entered. Exported because
// "backout" is a public grammar verb; feature packages may drive a Select's
// backout from feature-level lifecycle code (e.g. Escape fallbacks).
func (s *Select) Backout() {
	if !s.IsEntered() {
		return
	}

	// Exiting removes the selected descendants. Declare the surviving shell as
	// the next selection target before that subtree disappears; SelectableRow
	// owns the framework-level one-shot handoff during reconciliation.
	s.selectShellNext = true
	if s.props.Entered == nil {
		s.entered.Set(false)
	}

	if s.props.OnBackout != nil {
		s.props.OnBackout()
	}
	s.projectedEntered = s.IsEntered()
}

// Back closes Select's entered body through the same transition as its shell.
// Body-specific handlers may consume deeper local states before page fallback
// reaches this semantic owner.
func (s *Select) Back() bool {
	if !s.IsEntered() {
		return false
	}
	s.Backout()
	return true
}

// IsEntered returns whether the select is currently in entered state.
func (s *Select) IsEntered() bool {
	if s.props.Entered != nil {
		return s.props.Entered()
	}
	return s.entered.Get()
}

var (
	_ tui.Component    = (*Select)(nil)
	_ tui.Initializer  = (*Select)(nil)
	_ tui.AppBinder    = (*Select)(nil)
	_ tui.PropsUpdater = (*Select)(nil)
)
