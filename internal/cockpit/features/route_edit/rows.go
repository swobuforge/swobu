package route_edit

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

const (
	// Route edit keeps the wider arrow gutter used by the nested route plane so
	// the visible cursor lines up with the existing detail grammar.
	routeEditRowArrowWidth = 8
	routeEditRowLabelWidth = 15
)

type routeModelRowView struct {
	ui.SelectBase
	*Workflow
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
}

func (r *routeModelRowView) activate() {
	r.Workflow.ActivateName()
}

func (r *routeModelRowView) KeyMap() tui.KeyMap {
	return r.WithTraversal(ui.ActivateFocused(func(tui.KeyEvent) {
		r.activate()
	}))
}

func (r *routeModelRowView) Render(app *tui.App) *tui.Element {
	w := r.Workflow
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100.00),
		tui.WithFocusable(true),
		tui.WithOnFocus(r.OnFocus),
		tui.WithOnBlur(r.OnBlur),
		tui.WithOnActivate(r.activate),
	)
	root.AddChild(tui.New(
		tui.WithText(r.ArrowWithActiveDescendant(w.IsEditing())),
		tui.WithWidth(routeEditRowArrowWidth),
	))
	root.AddChild(tui.New(
		tui.WithText("model"),
		tui.WithWidth(routeEditRowLabelWidth),
	))
	if w.IsEditing() {
		root.AddChild(app.MountPersistent(r, 0, func() tui.Component {
			return tui.NewInput(
				tui.WithInputValue(w.ModelName),
				tui.WithInputAutoFocus(true),
				tui.WithInputOnSubmit(func(string) {
					r.activate()
				}),
				tui.WithInputWidth(ui.ActionRowValueWidth),
			)
		}))
	} else {
		root.AddChild(tui.New(
			tui.WithText(w.Route.ModelName),
			tui.WithWidth(ui.ActionRowValueWidth),
		))
	}
	root.AddChild(tui.New(tui.WithText(w.ActionLabel())))
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

func (r *routeActionRowView) Render(app *tui.App) *tui.Element {
	_ = app
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100.00),
		tui.WithFocusable(true),
		tui.WithOnFocus(r.OnFocus),
		tui.WithOnBlur(r.OnBlur),
		tui.WithOnActivate(r.activate),
	)
	root.AddChild(tui.New(
		tui.WithText(r.Arrow()),
		tui.WithWidth(routeEditRowArrowWidth),
	))
	root.AddChild(tui.New(
		tui.WithText(r.label()),
		tui.WithWidth(routeEditRowLabelWidth),
	))
	root.AddChild(tui.New(
		tui.WithText(r.value()),
		tui.WithWidth(ui.ActionRowValueWidth),
	))
	root.AddChild(tui.New(tui.WithText(r.action())))
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
