package routes

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

// RouteDetailRow renders the inline detail rows for an expanded route:
// name (editable), default toggle, and delete confirmation.
// It is a struct component owned by the section, cached per route, and
// mounts its own child rows as selectable rows with EditRowComponent.
type RouteDetailRow struct {
	ui.SelectBase
	RouteID      readmodel.RouteID
	ModelName    string // canonical persisted value, seeds edit mode
	IsDefault    bool
	OnSubmitName func(string)
	OnSetDefault func()
	OnDelete     func()

	rawName       *tui.State[string]
	editingName   *tui.State[bool]
	confirmDelete *tui.State[bool]
	errorText     *tui.State[string]
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

func (r *RouteDetailRow) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)
	// Name row (editable)
	root.AddChild(r.renderNameRow(app))
	// Default row (selectable action)
	root.AddChild(r.renderDefaultRow(app))
	// Delete row (selectable action with confirm)
	root.AddChild(r.renderDeleteRow(app))
	if r.errorText.Get() != "" {
		root.AddChild(r.renderErrorRow(r.errorText.Get()))
	}
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return root
}

func (r *RouteDetailRow) renderNameRow(app *tui.App) *tui.Element {
	if r.editingName.Get() {
		return renderEditableRow(app,
			targetDetailArrowWidth,
			"name",
			r.rawName,
			func() { r.CloseNameEdit() },
			func(val string) {
				if strings.TrimSpace(val) == "" {
					r.errorText.Set("enter a route model")
					return
				}
				if r.OnSubmitName != nil {
					r.OnSubmitName(val)
				}
			},
		)
	}
	return renderSelectableRow(
		targetDetailArrowWidth,
		"name",
		r.ModelName,
		"edit ↵",
		func() { r.OpenNameEdit() },
	)
}

func (r *RouteDetailRow) renderDefaultRow(app *tui.App) *tui.Element {
	value := "no"
	action := "make default ↵"
	if r.IsDefault {
		value = "yes"
		action = "current"
	}
	return renderSelectableRow(
		targetDetailArrowWidth,
		"default",
		value,
		action,
		func() {
			if r.OnSetDefault != nil {
				r.OnSetDefault()
			}
		},
	)
}

func (r *RouteDetailRow) renderDeleteRow(app *tui.App) *tui.Element {
	if r.confirmDelete.Get() {
		return renderSelectableRow(
			targetDetailArrowWidth,
			"delete",
			"delete "+r.ModelName+"?",
			"confirm ↵",
			func() {
				if r.OnDelete != nil {
					r.OnDelete()
				}
			},
		)
	}
	return renderSelectableRow(
		targetDetailArrowWidth,
		"delete",
		r.ModelName,
		"delete ↵",
		func() { r.OpenDeleteConfirm() },
	)
}

func (r *RouteDetailRow) renderErrorRow(msg string) *tui.Element {
	row := tui.New(tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row), tui.WithWidthPercent(100))
	row.AddChild(tui.New(tui.WithWidth(targetDetailArrowWidth)))
	row.AddChild(tui.New(tui.WithText(msg), tui.WithWidth(ui.ActionRowValueWidth)))
	return row
}

func (r *RouteDetailRow) KeyMap() tui.KeyMap {
	return r.WithTraversal(tui.KeyMap{
		tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) {
			r.CloseAll()
		}),
	})
}

var (
	_ tui.Component    = (*RouteDetailRow)(nil)
	_ tui.KeyListener  = (*RouteDetailRow)(nil)
	_ tui.PropsUpdater = (*RouteDetailRow)(nil)
	_ tui.AppBinder    = (*RouteDetailRow)(nil)
)

// targetDetailArrowWidth gives the visual indent for route detail rows
// (same level as step headers and contract rows).
const targetDetailArrowWidth = 8

// renderSelectableRow builds a static selectable row with the same layout
// as ui.SelectableRow but without the SelectBase overhead for inert
// action-only rows.
func renderSelectableRow(arrowWidth int, label, value, action string, activate func()) *tui.Element {
	row := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
		tui.WithFocusable(true),
		tui.WithOnActivate(activate),
	)
	row.AddChild(tui.New(tui.WithText(""), tui.WithWidth(arrowWidth)))
	row.AddChild(tui.New(tui.WithText(label), tui.WithWidth(labelWidth)))
	row.AddChild(tui.New(
		tui.WithText(value),
		tui.WithWidth(ui.ActionRowValueWidth),
		tui.WithWrap(false),
		tui.WithTruncate(true),
	))
	row.AddChild(tui.New(tui.WithWidth(gapWidth)))
	row.AddChild(tui.New(tui.WithText(action)))
	return row
}

// renderEditableRow builds a row with a mounted text input for editing.
func renderEditableRow(app *tui.App, arrowWidth int, label string, value *tui.State[string], onCancel func(), onSubmit func(string)) *tui.Element {
	row := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
	)
	row.AddChild(tui.New(tui.WithText(""), tui.WithWidth(arrowWidth)))
	row.AddChild(tui.New(tui.WithText(label), tui.WithWidth(labelWidth)))

	input := app.MountPersistent(nil, 0, func() tui.Component {
		return tui.NewInput(
			tui.WithInputValue(value),
			tui.WithInputAutoFocus(true),
			tui.WithInputOnSubmit(func(val string) {
				onSubmit(val)
			}),
			tui.WithInputWidth(ui.ActionRowValueWidth),
		)
	})
	row.AddChild(input)
	row.AddChild(tui.New(tui.WithText("save ↵")))
	return row
}
