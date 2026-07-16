package route_edit

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

// routeModelRowView renders the model-name row for route editing.
// Focus never leaves the row shell; the text surface is an InlineEditor.
type routeModelRowView struct {
	ui.SelectBase
	*Workflow
	editor *ui.InlineEditor
}

func routeModelRowKey(w *Workflow) string {
	return "route-edit:" + string(w.Route.ID) + ":model"
}

func ModelRowComponent(w *Workflow) tui.Component {
	return &routeModelRowView{
		SelectBase: ui.NewSelectBase(routeModelRowKey(w)),
		Workflow:   w,
	}
}

func (r *routeModelRowView) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*routeModelRowView)
	if !ok {
		return
	}
	r.Workflow = f.Workflow
	r.ID = routeModelRowKey(f.Workflow)
	// Preserve editor so typing survives re-render.
}

func (r *routeModelRowView) BindApp(app *tui.App) {
	r.SelectBase.BindApp(app)
	if r.editor != nil {
		r.editor.BindApp(app)
	}
}

func (r *routeModelRowView) activate() {
	r.Workflow.ActivateName()
}

func (r *routeModelRowView) KeyMap() tui.KeyMap {
	w := r.Workflow
	if !w.IsEditing() {
		return r.WithTraversal(ui.ActivateFocused(func(tui.KeyEvent) {
			r.activate()
		}))
	}
	// Edit mode: Escape backs out; typing keys forward into InlineEditor.
	km := tui.KeyMap{tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) { w.Back() })}
	return r.WithTraversal(append(km, r.editor.TypingKeyMap()...))
}

func (r *routeModelRowView) Render(_ *tui.App) *tui.Element {
	w := r.Workflow
	if w.IsEditing() {
		// Lazily create InlineEditor on first edit open so it binds to the
		// correct app. Open sets cursor text from ModelName.
		if r.editor == nil {
			r.editor = ui.NewInlineEditor(w.ModelName)
			r.editor.Width = ui.ActionRowValueWidth
			r.editor.OnSubmit = func(_ string) {
				if strings.TrimSpace(w.ModelName.Get()) == "" {
					return
				}
				r.Workflow.ActivateName()
			}
			if app := r.App(); app != nil {
				r.editor.BindApp(app)
			}
		}
		r.editor.SetText(w.ModelName.Get())
		root := ui.EditRow(
			r.SelectBase.Arrow(), "model", r.editor.Render(), w.ActionLabel(),
			tui.WithFocusable(true),
			tui.WithOnFocus(r.OnFocus),
			tui.WithOnBlur(r.OnBlur),
			tui.WithOnActivate(r.activate),
		)
		if r.Ref != nil {
			r.Ref.Set(root)
		}
		return root
	}

	root := ui.ActionRow(
		r.Arrow(), "model", w.Route.ModelName, w.ActionLabel(),
		tui.WithFocusable(true),
		tui.WithOnFocus(r.OnFocus),
		tui.WithOnBlur(r.OnBlur),
		tui.WithOnActivate(r.activate),
	)
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return root
}

type routeActionRowKind int

const (
	routeActionDefault routeActionRowKind = iota
	routeActionDelete
)

type routeActionRowView struct {
	ui.SelectBase
	*Workflow
	kind routeActionRowKind
}

func routeActionRowKey(w *Workflow, kind routeActionRowKind) string {
	suffix := "default"
	if kind == routeActionDelete {
		suffix = "delete"
	}
	return "route-edit:" + string(w.Route.ID) + ":" + suffix
}

func DefaultRowComponent(w *Workflow) tui.Component {
	return &routeActionRowView{
		SelectBase: ui.NewSelectBase(routeActionRowKey(w, routeActionDefault)),
		Workflow:   w,
		kind:       routeActionDefault,
	}
}

func DeleteRowComponent(w *Workflow) tui.Component {
	return &routeActionRowView{
		SelectBase: ui.NewSelectBase(routeActionRowKey(w, routeActionDelete)),
		Workflow:   w,
		kind:       routeActionDelete,
	}
}

func (r *routeActionRowView) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*routeActionRowView)
	if !ok {
		return
	}
	r.Workflow = f.Workflow
	r.kind = f.kind
	r.ID = routeActionRowKey(f.Workflow, f.kind)
}

func (r *routeActionRowView) activate() {
	switch r.kind {
	case routeActionDefault:
		r.Workflow.ActivateDefault()
	case routeActionDelete:
		r.Workflow.ActivateDelete()
	}
}

func (r *routeActionRowView) KeyMap() tui.KeyMap {
	return r.WithTraversal(ui.ActivateFocused(func(tui.KeyEvent) {
		r.activate()
	}))
}

func (r *routeActionRowView) label() string {
	switch r.kind {
	case routeActionDefault:
		return "default"
	case routeActionDelete:
		return "delete"
	default:
		return ""
	}
}

func (r *routeActionRowView) value() string {
	switch r.kind {
	case routeActionDefault:
		return r.Workflow.DefaultValueLabel()
	case routeActionDelete:
		return r.Workflow.DeleteValueLabel()
	default:
		return ""
	}
}

func (r *routeActionRowView) action() string {
	switch r.kind {
	case routeActionDefault:
		return r.Workflow.DefaultActionLabel()
	case routeActionDelete:
		return r.Workflow.DeleteActionLabel()
	default:
		return ""
	}
}

func (r *routeActionRowView) Render(_ *tui.App) *tui.Element {
	root := ui.ActionRow(
		r.Arrow(), r.label(), r.value(), r.action(),
		tui.WithFocusable(true),
		tui.WithOnFocus(r.OnFocus),
		tui.WithOnBlur(r.OnBlur),
		tui.WithOnActivate(r.activate),
	)
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return root
}

var (
	_ tui.Component    = (*routeModelRowView)(nil)
	_ tui.KeyListener  = (*routeModelRowView)(nil)
	_ tui.AppBinder    = (*routeModelRowView)(nil)
	_ tui.PropsUpdater = (*routeModelRowView)(nil)
	_ tui.Component    = (*routeActionRowView)(nil)
	_ tui.KeyListener  = (*routeActionRowView)(nil)
	_ tui.AppBinder    = (*routeActionRowView)(nil)
	_ tui.PropsUpdater = (*routeActionRowView)(nil)
)
