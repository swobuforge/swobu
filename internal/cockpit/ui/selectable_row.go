package ui

import tui "github.com/grindlemire/go-tui"

const (
	selectableRowArrowWidth = 5
	selectableRowLabelWidth = 18
	// ActionRowValueWidth keeps action-bearing cockpit rows on one shared
	// value column so verbs start in the same vertical line.
	ActionRowValueWidth         = 35
	selectableRowActionGapWidth = 1
)

// SelectableRow is a selectable action row with focus markers and activation.
// Sections mount this via app.Mount() so it participates in the go-tui focus
// graph and has KeyMap-based activation.
type SelectableRow struct {
	SelectBase
	Label     string
	Value     string
	Action    string
	Activate  func()
	OnCancel  func() // If set, Escape fires this when the row is focused.
	AutoFocus bool
	// ArrowPad increases the marker cell width so nested rows can keep their
	// marker aligned with the row's own indent.
	ArrowPad int
}

// NewSelectableRow builds a mountable selectable row.
func NewSelectableRow(id, label, value, action string, activate func()) *SelectableRow {
	return &SelectableRow{
		SelectBase: NewSelectBase(id),
		Label:      label,
		Value:      value,
		Action:     action,
		Activate:   activate,
	}
}

func (r *SelectableRow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*SelectableRow)
	if !ok {
		return
	}

	r.Label = f.Label
	r.Value = f.Value
	r.Action = f.Action
	r.Activate = f.Activate
	r.OnCancel = f.OnCancel
	r.AutoFocus = f.AutoFocus
	r.ArrowPad = f.ArrowPad
}

func (r *SelectableRow) Init() func() {
	if !r.AutoFocus {
		return nil
	}

	// Seed the visible marker immediately so the first render can show the
	// child row as focused even though go-tui resolves the fresh ref only
	// after render. The queued focus handoff then aligns the real focus
	// manager with that visible trap state.
	r.focused.Set(true)
	if r.app != nil {
		r.Focus(r.app)
	}
	return nil
}

func (r *SelectableRow) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100.00),
		tui.WithFocusable(true),
		tui.WithAutoFocus(r.AutoFocus),
		tui.WithOnFocus(r.OnFocus),
		tui.WithOnBlur(r.OnBlur),
		tui.WithOnActivate(r.Activate),
	)
	arrowWidth := selectableRowArrowWidth + r.ArrowPad
	if arrowWidth < 1 {
		arrowWidth = 1
	}
	root.AddChild(tui.New(tui.WithText(r.Arrow()), tui.WithWidth(arrowWidth)))
	root.AddChild(tui.New(tui.WithText(r.Label), tui.WithWidth(selectableRowLabelWidth)))
	// Keep the value field single-line and leave one explicit separator cell
	// before the action label so long values cannot bleed into the action.
	root.AddChild(tui.New(
		tui.WithText(r.Value),
		tui.WithWidth(ActionRowValueWidth),
		tui.WithWrap(false),
		tui.WithTruncate(true),
	))
	root.AddChild(tui.New(tui.WithWidth(selectableRowActionGapWidth)))
	root.AddChild(tui.New(tui.WithText(r.Action)))
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return root
}

func (r *SelectableRow) KeyMap() tui.KeyMap {
	km := r.WithTraversal(ActivateFocused(func(tui.KeyEvent) {
		if r.Activate != nil {
			r.Activate()
		}
	}))
	if r.OnCancel != nil {
		km = append(km, tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) {
			r.OnCancel()
		}))
	}
	return km
}

var (
	_ tui.Component    = (*SelectableRow)(nil)
	_ tui.KeyListener  = (*SelectableRow)(nil)
	_ tui.AppBinder    = (*SelectableRow)(nil)
	_ tui.Initializer  = (*SelectableRow)(nil)
	_ tui.PropsUpdater = (*SelectableRow)(nil)
)
