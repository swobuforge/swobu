package ui

import tui "github.com/grindlemire/go-tui"

// RowIndent is the parent-declared left padding (in cells) for all child rows
// inside a section. The section adds this as padding-left on a wrapper div;
// children render at the padded boundary without adding their own indent spans.
const RowIndent = 3

// SelectableRow is a selectable action row with focus markers and activation.
// Sections mount this via app.Mount() so it participates in the go-tui focus
// graph and has KeyMap-based activation.
//
// Escape is NOT handled here. SelectableRow is a leaf component; Escape
// belongs to the containing FocusableControl, which calls the cancellation
// callback from OnExit.
type SelectableRow struct {
	SelectBase
	Label     string
	Value     string
	Action    string
	Activate  func()
	// AutoFocus seeds the row as selected on mount, or on the first transition
	// from false to true on an already-mounted row.
	AutoFocus bool
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

	prevAutoFocus := r.AutoFocus
	r.Label = f.Label
	r.Value = f.Value
	r.Action = f.Action
	r.Activate = f.Activate
	r.AutoFocus = f.AutoFocus

	if !prevAutoFocus && r.AutoFocus && !r.IsFocused() {
		r.Focus(r.App())
	}
}

// Arrow returns the selected-row marker. AutoFocus is treated as a declarative
// selected seed so the marker can appear immediately while the focus graph
// catches up.
func (r *SelectableRow) Arrow() string {
	if r.AutoFocus {
		return SelectArrowFocused
	}
	return r.SelectBase.Arrow()
}

func (r *SelectableRow) Init() func() {
	if !r.AutoFocus || r.IsFocused() {
		return nil
	}

	// Seed the visible marker once on mount so the first frame can show the
	// selected row immediately. AutoFocus is a mount/update hint, not a
	// per-render side effect.
	r.focused.Set(true)
	if r.app != nil {
		r.Focus(r.app)
	}
	return nil
}

func (r *SelectableRow) Render(app *tui.App) *tui.Element {
	// Render stays pure; one-shot autofocus seeding happens in Init or
	// UpdateProps, while Arrow() may read the declarative seed to draw the
	// marker immediately.
	root := ActionRow(r.Arrow(), r.Label, r.Value, r.Action,
		tui.WithFocusable(true),
		tui.WithAutoFocus(r.AutoFocus),
		tui.WithOnFocus(r.OnFocus),
		tui.WithOnBlur(r.OnBlur),
		tui.WithOnActivate(r.Activate),
	)
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return root
}

// KeyMap returns the keyboard bindings for activation and traversal.
// Escape is NOT handled here — the containing FocusableControl owns it.
func (r *SelectableRow) KeyMap() tui.KeyMap {
	return r.WithTraversal(ActivateFocused(func(tui.KeyEvent) {
		if r.Activate != nil {
			r.Activate()
		}
	}))
}

var (
	_ tui.Component    = (*SelectableRow)(nil)
	_ tui.KeyListener  = (*SelectableRow)(nil)
	_ tui.AppBinder    = (*SelectableRow)(nil)
	_ tui.Initializer  = (*SelectableRow)(nil)
	_ tui.PropsUpdater = (*SelectableRow)(nil)
)
