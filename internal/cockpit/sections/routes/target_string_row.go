package routes

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

// TargetStringRow replaces the old target_edit workflow rendered rows for
// both existing-target edit and new-target create. It renders as a
// selectable row in view mode, and as a text input in edit mode.
//
// Focus never leaves the row shell; the text surface is an InlineEditor.
type TargetStringRow struct {
	ui.SelectBase
	Label     string
	saved     string
	raw       *tui.State[string]
	editing   *tui.State[bool]
	errorText *tui.State[string]
	onSubmit  func(string)
	onDelete  func()
	onCancel  func()
	isCreate  bool
	editor    *ui.InlineEditor
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

func NewTargetCreateRow(id string, onSubmit func(string), onCancel func()) *TargetStringRow {
	return &TargetStringRow{
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
}

func (r *TargetStringRow) Open() {
	r.editing.Set(true)
	if r.isCreate {
		r.raw.Set("")
	} else {
		r.raw.Set(r.saved)
	}
	r.errorText.Set("")
	if r.editor != nil {
		r.editor.SetText(r.raw.Get())
	}
}

func (r *TargetStringRow) Close() {
	r.editing.Set(false)
	r.errorText.Set("")
	if r.isCreate && r.onCancel != nil {
		r.onCancel()
	}
}

func (r *TargetStringRow) SetSaved(saved string) {
	r.saved = saved
}

func (r *TargetStringRow) ErrorText() string {
	return r.errorText.Get()
}

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
	if r.editor != nil {
		r.editor.BindApp(app)
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

func (r *TargetStringRow) Render(_ *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)
	if r.isCreate || r.editing.Get() {
		root.AddChild(r.renderEdit())
	} else {
		root.AddChild(r.renderView())
	}
	if err := r.errorText.Get(); err != "" {
		root.AddChild(r.renderError(err))
	}
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return root
}

func (r *TargetStringRow) renderView() *tui.Element {
	return ui.ActionRow(r.Arrow(), r.Label, "", "edit ↵",
		tui.WithFocusable(true),
		tui.WithOnFocus(r.OnFocus),
		tui.WithOnBlur(r.OnBlur),
		tui.WithOnActivate(r.Open),
	)
}

func (r *TargetStringRow) renderEdit() *tui.Element {
	if r.editor == nil {
		r.editor = ui.NewInlineEditor(r.raw)
		r.editor.Width = ui.ActionRowValueWidth
		r.editor.OnSubmit = func(_ string) {
			r.submit(r.raw.Get())
		}
		if app := r.App(); app != nil {
			r.editor.BindApp(app)
		}
	}
	action := "save ↵"
	if r.errorText.Get() != "" {
		action = "invalid"
	}
	return ui.EditRow(r.SelectBase.Arrow(), "", r.editor.Render(), action)
}

func (r *TargetStringRow) renderError(msg string) *tui.Element {
	return ui.ActionRow("", "", msg, "")
}

func (r *TargetStringRow) submit(val string) {
	if r.onSubmit != nil {
		r.onSubmit(val)
	}
}

func (r *TargetStringRow) KeyMap() tui.KeyMap {
	if !r.IsEditing() {
		return r.WithTraversal(ui.ActivateFocused(func(tui.KeyEvent) {
			r.Open()
		}))
	}
	km := tui.KeyMap{
		tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) { r.Close() }),
	}
	if r.editor == nil {
		return r.WithTraversal(km)
	}
	return r.WithTraversal(append(km, r.editor.TypingKeyMap()...))
}

var (
	_ tui.Component    = (*TargetStringRow)(nil)
	_ tui.KeyListener  = (*TargetStringRow)(nil)
	_ tui.PropsUpdater = (*TargetStringRow)(nil)
	_ tui.AppBinder    = (*TargetStringRow)(nil)
)

func targetLabel(route readmodel.RouteReadModel, target readmodel.TargetReadModel) string {
	value := targetValue(target)
	stepTargets := targetsAtRank(route.Targets, target.Rank)
	if len(stepTargets) > 1 {
		value = value + " · " + sharePercent(target, stepTargets) + "%"
	}
	return value
}
