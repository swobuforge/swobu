// ui provides shared presentation types for cockpit sections.
//
// Row components (FocusableRowView, FocusablePreviewRow, InertRow, DetailRow)
// are defined here as pure Go types so each section can compose them without
// repeating the full struct definition, constructor, and lifecycle methods.
//
// Go-tui gsx cross-package component references are generator-broken, so
// sections keep thin local wrapper functions with a templ Render() body.
package ui

import tui "github.com/grindlemire/go-tui"

// --- FocusableRowView ---

// FocusableRowView is an interactive row with focus/blur/marker state.
type FocusableRowView struct {
	Label    string
	Value    string
	Action   string
	Activate func()
	focused  *tui.State[bool]
}

// NewFocusableRowView builds a focus-managed row component.
func NewFocusableRowView(label, value, action string, activate func()) *FocusableRowView {
	return &FocusableRowView{
		Label:    label,
		Value:    value,
		Action:   action,
		Activate: activate,
		focused:  tui.NewState(false),
	}
}

// Focus marks the row focused.
func (r *FocusableRowView) Focus(*tui.Element) { r.focused.Set(true) }

// Blur marks the row unfocused.
func (r *FocusableRowView) Blur(*tui.Element) { r.focused.Set(false) }

// Marker returns the row's focus indicator.
func (r *FocusableRowView) Marker() string {
	if r.focused.Get() {
		return ">"
	}
	return ""
}

// Render builds the interactive row element tree.
func (r *FocusableRowView) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100.00),
		tui.WithOnFocus(r.Focus),
		tui.WithOnBlur(r.Blur),
		tui.WithOnActivate(r.Activate),
	)
	root.AddChild(tui.New(tui.WithText(r.Marker()), tui.WithWidth(5)))
	root.AddChild(tui.New(tui.WithText(r.Label), tui.WithWidth(18)))
	root.AddChild(tui.New(tui.WithText(r.Value), tui.WithWidth(36)))
	root.AddChild(tui.New(tui.WithText(r.Action)))
	return root
}

// UpdateProps copies reconcilable fields from a fresh component.
func (r *FocusableRowView) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*FocusableRowView)
	if !ok {
		return
	}
	r.Label, r.Value, r.Action = f.Label, f.Value, f.Action
}

// BindApp wires the component's state to the app.
func (r *FocusableRowView) BindApp(app *tui.App) {
	if r.focused != nil {
		r.focused.BindApp(app)
	}
}

var (
	_ tui.Component    = (*FocusableRowView)(nil)
	_ tui.PropsUpdater = (*FocusableRowView)(nil)
	_ tui.AppBinder    = (*FocusableRowView)(nil)
)

// --- FocusablePreviewRow ---

// FocusablePreviewRow is a non-focusable row for static preview rendering.
type FocusablePreviewRow struct{ Root *tui.Element }

// NewFocusablePreviewRow builds a preview row without focus state.
func NewFocusablePreviewRow(label, value, action string, activate func()) *FocusablePreviewRow {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100.00),
		tui.WithOnActivate(activate),
	)
	root.AddChild(tui.New(tui.WithWidth(5)))
	root.AddChild(tui.New(tui.WithText(label), tui.WithWidth(18)))
	root.AddChild(tui.New(tui.WithText(value), tui.WithWidth(36)))
	root.AddChild(tui.New(tui.WithText(action)))
	return &FocusablePreviewRow{Root: root}
}

// Render returns the previously constructed element tree.
func (r *FocusablePreviewRow) Render(app *tui.App) *tui.Element { return r.Root }

var _ tui.Component = (*FocusablePreviewRow)(nil)

// --- InertRow ---

// InertRow is a non-interactive static row.
type InertRow struct{ Root *tui.Element }

// NewInertRow builds a read-only row.
func NewInertRow(label, value, action string) *InertRow {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100.00),
	)
	root.AddChild(tui.New(tui.WithWidth(5)))
	root.AddChild(tui.New(tui.WithText(label), tui.WithWidth(18)))
	root.AddChild(tui.New(tui.WithText(value), tui.WithWidth(36)))
	root.AddChild(tui.New(tui.WithText(action)))
	return &InertRow{Root: root}
}

// Render returns the previously constructed element tree.
func (r *InertRow) Render(app *tui.App) *tui.Element { return r.Root }

var _ tui.Component = (*InertRow)(nil)

// --- DetailRow ---

// DetailRow is a read-only detail line with extra left indentation.
type DetailRow struct{ Root *tui.Element }

// NewDetailRow builds a detail row.
func NewDetailRow(label, value string) *DetailRow {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100.00),
	)
	root.AddChild(tui.New(tui.WithWidth(8)))
	root.AddChild(tui.New(tui.WithText(label), tui.WithWidth(15)))
	root.AddChild(tui.New(tui.WithText(value)))
	return &DetailRow{Root: root}
}

// Render returns the previously constructed element tree.
func (r *DetailRow) Render(app *tui.App) *tui.Element { return r.Root }

var _ tui.Component = (*DetailRow)(nil)
