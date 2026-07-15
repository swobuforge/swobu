package ui

import tui "github.com/grindlemire/go-tui"

// SelectableRow is a selectable action row with focus markers and activation.
// Sections mount this via app.Mount() so it participates in the go-tui focus
// graph and has KeyMap-based activation.
type SelectableRow struct {
	SelectBase
	Label     string
	Value     string
	Action    string
	Activate  func()
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
	root.AddChild(tui.New(tui.WithText(r.Arrow()), tui.WithWidth(2)))
	root.AddChild(tui.New(tui.WithText(r.Label), tui.WithWidth(18)))
	root.AddChild(tui.New(tui.WithText(r.Value), tui.WithWidth(36)))
	root.AddChild(tui.New(tui.WithText(r.Action)))
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return root
}

func (r *SelectableRow) KeyMap() tui.KeyMap {
	keymap := ActivateFocused(func(tui.KeyEvent) {
		if r.Activate != nil {
			r.Activate()
		}
	})
	return append(keymap,
		tui.OnFocused(tui.KeyDown, MoveNext),
		tui.OnFocused(tui.KeyUp, MovePrev),
	)
}

var (
	_ tui.Component   = (*SelectableRow)(nil)
	_ tui.KeyListener = (*SelectableRow)(nil)
	_ tui.AppBinder   = (*SelectableRow)(nil)
)
