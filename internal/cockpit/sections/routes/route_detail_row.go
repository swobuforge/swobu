package routes

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

// RouteDetailRow renders the inline detail rows for an expanded route:
// name (editable), default toggle, and delete confirmation.
//
// The name row uses InlineEditor so focus never leaves the row shell.
type RouteDetailRow struct {
	ui.SelectBase
	RouteID      readmodel.RouteID
	ModelName    string
	IsDefault    bool
	OnSubmitName func(string)
	OnSetDefault func()
	OnDelete     func()

	rawName       *tui.State[string]
	editingName   *tui.State[bool]
	confirmDelete *tui.State[bool]
	errorText     *tui.State[string]
	editor        *ui.InlineEditor
}

func NewRouteDetailRow(id string, modelName string, isDefault bool, onSubmitName func(string), onSetDefault func(), onDelete func()) *RouteDetailRow {
	return &RouteDetailRow{
		SelectBase:    ui.NewSelectBase(id),
		ModelName:     modelName,
		IsDefault:     isDefault,
		OnSubmitName:  onSubmitName,
		OnSetDefault:  onSetDefault,
		OnDelete:      onDelete,
		rawName:       tui.NewState(modelName),
		editingName:   tui.NewState(false),
		confirmDelete: tui.NewState(false),
		errorText:     tui.NewState(""),
	}
}

func (r *RouteDetailRow) OpenNameEdit() {
	r.editingName.Set(true)
	r.rawName.Set(r.ModelName)
	r.errorText.Set("")
	if r.editor != nil {
		r.editor.SetText(r.ModelName)
	}
}

func (r *RouteDetailRow) CloseNameEdit() {
	r.editingName.Set(false)
	r.errorText.Set("")
}

func (r *RouteDetailRow) OpenDeleteConfirm() {
	r.confirmDelete.Set(true)
	r.errorText.Set("")
}

func (r *RouteDetailRow) CloseDeleteConfirm() {
	r.confirmDelete.Set(false)
	r.errorText.Set("")
}

func (r *RouteDetailRow) CloseAll() {
	r.editingName.Set(false)
	r.confirmDelete.Set(false)
	r.errorText.Set("")
}

func (r *RouteDetailRow) SetModelName(name string) {
	r.ModelName = name
}

func (r *RouteDetailRow) ErrorText() string { return r.errorText.Get() }

func (r *RouteDetailRow) IsEditingName() bool   { return r.editingName.Get() }
func (r *RouteDetailRow) IsConfirmDelete() bool { return r.confirmDelete.Get() }

func (r *RouteDetailRow) BindApp(app *tui.App) {
	r.SelectBase.BindApp(app)
	if r.rawName != nil {
		r.rawName.BindApp(app)
	}
	if r.editingName != nil {
		r.editingName.BindApp(app)
	}
	if r.confirmDelete != nil {
		r.confirmDelete.BindApp(app)
	}
	if r.errorText != nil {
		r.errorText.BindApp(app)
	}
	if r.editor != nil {
		r.editor.BindApp(app)
	}
}

func (r *RouteDetailRow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*RouteDetailRow)
	if !ok {
		return
	}
	r.RouteID = f.RouteID
	if !r.IsEditingName() {
		r.ModelName = f.ModelName
	}
	r.IsDefault = f.IsDefault
	r.OnSubmitName = f.OnSubmitName
	r.OnSetDefault = f.OnSetDefault
	r.OnDelete = f.OnDelete
}

func (r *RouteDetailRow) Render(_ *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)
	root.AddChild(r.renderNameRow())
	root.AddChild(r.renderDefaultRow())
	root.AddChild(r.renderDeleteRow())
	if r.errorText.Get() != "" {
		root.AddChild(r.renderErrorRow(r.errorText.Get()))
	}
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return root
}

func (r *RouteDetailRow) renderNameRow() *tui.Element {
	if r.editingName.Get() {
		if r.editor == nil {
			r.editor = ui.NewInlineEditor(r.rawName)
			r.editor.Width = ui.ActionRowValueWidth
			r.editor.OnSubmit = func(_ string) {
				if strings.TrimSpace(r.rawName.Get()) == "" {
					r.errorText.Set("enter a route model")
					return
				}
				if r.OnSubmitName != nil {
					r.OnSubmitName(r.rawName.Get())
				}
			}
			if app := r.App(); app != nil {
				r.editor.BindApp(app)
			}
		}
		return ui.EditRow("", "name", r.editor.Render(), "save ↵",
			tui.WithFocusable(true),
			tui.WithOnActivate(r.CloseNameEdit),
		)
	}
	return ui.ActionRow("", "name", r.ModelName, "edit ↵",
		tui.WithFocusable(true),
		tui.WithOnActivate(r.OpenNameEdit),
	)
}

func (r *RouteDetailRow) renderDefaultRow() *tui.Element {
	value := "no"
	action := "make default ↵"
	if r.IsDefault {
		value = "yes"
		action = "current"
	}
	return ui.ActionRow("", "default", value, action,
		tui.WithFocusable(true),
		tui.WithOnActivate(func() {
			if r.OnSetDefault != nil {
				r.OnSetDefault()
			}
		}),
	)
}

func (r *RouteDetailRow) renderDeleteRow() *tui.Element {
	if r.confirmDelete.Get() {
		return ui.ActionRow("", "delete", "delete "+r.ModelName+"?", "confirm ↵",
			tui.WithFocusable(true),
			tui.WithOnActivate(func() {
				if r.OnDelete != nil {
					r.OnDelete()
				}
			}),
		)
	}
	return ui.ActionRow("", "delete", r.ModelName, "delete ↵",
		tui.WithFocusable(true),
		tui.WithOnActivate(r.OpenDeleteConfirm),
	)
}

func (r *RouteDetailRow) renderErrorRow(msg string) *tui.Element {
	return ui.ActionRow("", "", msg, "")
}

func (r *RouteDetailRow) KeyMap() tui.KeyMap {
	if r.IsEditingName() {
		km := tui.KeyMap{
			tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) { r.CloseNameEdit() }),
		}
		if r.editor == nil {
			return km
		}
		return r.WithTraversal(append(km, r.editor.TypingKeyMap()...))
	}
	return r.WithTraversal(ui.ActivateFocused(func(tui.KeyEvent) {
		r.OpenNameEdit()
	}))
}

var (
	_ tui.Component    = (*RouteDetailRow)(nil)
	_ tui.KeyListener  = (*RouteDetailRow)(nil)
	_ tui.PropsUpdater = (*RouteDetailRow)(nil)
	_ tui.AppBinder    = (*RouteDetailRow)(nil)
)
