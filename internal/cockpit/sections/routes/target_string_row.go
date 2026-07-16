package routes

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

// ui layout constants — duplicated here because ui.selectableRow* are
// unexported. They match the SelectableRow / EditableRow layout exactly.
const (
	arrowBaseWidth = 5  // matches ui.selectableRowArrowWidth
	labelWidth     = 18 // matches ui.selectableRowLabelWidth
	gapWidth       = 1  // matches ui.selectableRowActionGapWidth
)

// TargetStringRow replaces the old target_edit workflow rendered rows for
// both existing-target edit and new-target create. It renders as a
// selectable row in view mode, and as a text input in edit mode. The
// isCreate flag puts the row into create mode: it always renders the input
// (no view mode) and cancels by calling onCancel instead of returning to
// view.
type TargetStringRow struct {
	ui.SelectBase
	// Label is the displayed value including share% suffix (e.g.
	// "openai/gpt-4.1 · 50%"). It is computed by the section on each render.
	// Unused when isCreate is true.
	Label string
	// saved is the canonical provider/model string used as the default
	// when opening edit mode and as the view-mode display text.
	saved     string
	raw       *tui.State[string]
	editing   *tui.State[bool]
	errorText *tui.State[string]
	onSubmit  func(string)
	onDelete  func()
	onCancel  func()
	isCreate  bool
}

func NewTargetStringRow(id, label, saved string, onSubmit func(string), onDelete func()) *TargetStringRow {
	return &TargetStringRow{
		SelectBase: ui.NewSelectBase(id),
		Label:      label,
		saved:      saved,
		raw:        tui.NewState(saved),
		editing:    tui.NewState(false),
		errorText:  tui.NewState(""),
		onSubmit:   onSubmit,
		onDelete:   onDelete,
	}
}

// NewTargetCreateRow builds a mountable target-string create row that shows
// the input immediately (no view mode) and calls onCancel on Escape.
func NewTargetCreateRow(id string, onSubmit func(string), onCancel func()) *TargetStringRow {
	r := &TargetStringRow{
		SelectBase: ui.NewSelectBase(id),
		Label:      "",
		saved:      "",
		raw:        tui.NewState(""),
		editing:    tui.NewState(true),
		errorText:  tui.NewState(""),
		onSubmit:   onSubmit,
		onCancel:   onCancel,
		isCreate:   true,
	}
	return r
}

// Open switches the row into edit mode and seeds the input. For create
// mode the input starts blank; for edit mode it starts from the saved
// value.
func (r *TargetStringRow) Open() {
	r.editing.Set(true)
	if r.isCreate {
		r.raw.Set("")
	} else {
		r.raw.Set(r.saved)
	}
	r.errorText.Set("")
}

// Close returns the row to view mode and clears any error. In create mode
// it notifies the section via onCancel so the transient row disappears.
func (r *TargetStringRow) Close() {
	r.editing.Set(false)
	r.errorText.Set("")
	if r.isCreate && r.onCancel != nil {
		r.onCancel()
	}
}

// SetSaved updates the canonical saved value. Called by the section on each
// render via UpdateProps so the row shows the latest persisted value.
func (r *TargetStringRow) SetSaved(saved string) {
	r.saved = saved
}

// ErrorText returns the current validation error message. The section uses
// this inside GSX conditionals to decide whether to render an error row.
func (r *TargetStringRow) ErrorText() string {
	return r.errorText.Get()
}

// IsEditing returns true when the input is visible.
func (r *TargetStringRow) IsEditing() bool {
	return r.editing.Get()
}

func (r *TargetStringRow) BindApp(app *tui.App) {
	r.SelectBase.BindApp(app)
	if r.raw != nil {
		r.raw.BindApp(app)
	}
	if r.editing != nil {
		r.editing.BindApp(app)
	}
	if r.errorText != nil {
		r.errorText.BindApp(app)
	}
}

func (r *TargetStringRow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*TargetStringRow)
	if !ok {
		return
	}
	r.Label = f.Label
	r.saved = f.saved
	r.onSubmit = f.onSubmit
	r.onDelete = f.onDelete
	r.onCancel = f.onCancel
	r.isCreate = f.isCreate
}

func (r *TargetStringRow) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)
	if r.isCreate || r.editing.Get() {
		root.AddChild(r.renderEdit(app))
	} else {
		root.AddChild(r.renderView(app))
	}
	if err := r.errorText.Get(); err != "" {
		root.AddChild(r.renderError(err))
	}
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return root
}

func (r *TargetStringRow) renderView(app *tui.App) *tui.Element {
	row := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
		tui.WithFocusable(true),
		tui.WithOnFocus(r.OnFocus),
		tui.WithOnBlur(r.OnBlur),
		tui.WithOnActivate(func() { r.Open() }),
	)
	// ArrowPad=5 gives visual indent 10, matching existing target rows.
	aw := arrowBaseWidth + 5
	if aw < 1 {
		aw = 1
	}
	row.AddChild(tui.New(tui.WithText(r.Arrow()), tui.WithWidth(aw)))
	row.AddChild(tui.New(tui.WithText(r.Label), tui.WithWidth(labelWidth)))
	row.AddChild(tui.New(
		tui.WithText(""),
		tui.WithWidth(ui.ActionRowValueWidth),
		tui.WithWrap(false),
	))
	row.AddChild(tui.New(tui.WithWidth(gapWidth)))
	row.AddChild(tui.New(tui.WithText("edit ↵")))
	return row
}

func (r *TargetStringRow) renderEdit(app *tui.App) *tui.Element {
	row := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
	)
	aw := arrowBaseWidth + 5
	if aw < 1 {
		aw = 1
	}
	row.AddChild(tui.New(tui.WithText(r.ArrowWithActiveDescendant(true)), tui.WithWidth(aw)))

	input := app.MountPersistent(r, 0, func() tui.Component {
		return tui.NewInput(
			tui.WithInputValue(r.raw),
			tui.WithInputAutoFocus(true),
			tui.WithInputOnSubmit(func(val string) {
				r.submit(val)
			}),
			tui.WithInputWidth(ui.ActionRowValueWidth),
		)
	})
	row.AddChild(input)

	action := "save ↵"
	if r.errorText.Get() != "" {
		action = "invalid"
	}
	row.AddChild(tui.New(tui.WithText(action)))
	return row
}

func (r *TargetStringRow) renderError(msg string) *tui.Element {
	row := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
	)
	aw := arrowBaseWidth + 5
	if aw < 1 {
		aw = 1
	}
	row.AddChild(tui.New(tui.WithWidth(aw)))
	row.AddChild(tui.New(tui.WithText(msg), tui.WithWidth(ui.ActionRowValueWidth)))
	return row
}

func (r *TargetStringRow) submit(val string) {
	if r.onSubmit != nil {
		r.onSubmit(val)
	}
}

func (r *TargetStringRow) KeyMap() tui.KeyMap {
	return r.WithTraversal(tui.KeyMap{
		tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) {
			if r.editing.Get() {
				r.Close()
			}
		}),
	})
}

var (
	_ tui.Component    = (*TargetStringRow)(nil)
	_ tui.KeyListener  = (*TargetStringRow)(nil)
	_ tui.PropsUpdater = (*TargetStringRow)(nil)
	_ tui.AppBinder    = (*TargetStringRow)(nil)
)

// targetLabel computes the display label for a target row, including the
// share percentage when the target is in a balanced (multi-target) step.
func targetLabel(route readmodel.RouteReadModel, target readmodel.TargetReadModel) string {
	value := targetValue(target)
	stepTargets := targetsAtRank(route.Targets, target.Rank)
	if len(stepTargets) > 1 {
		value = value + " · " + sharePercent(target, stepTargets) + "%"
	}
	return value
}
