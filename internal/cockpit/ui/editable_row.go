package ui

import tui "github.com/grindlemire/go-tui"

// EditableRow is a selectable row that reveals a mounted text input on
// activation. It owns the editing/viewing phase so callers only manage value
// state and submit behavior. The row marker (> ) stays visible while the child
// input receives typing.
type EditableRow struct {
	SelectBase
	Label      string
	Value      *tui.State[string]
	InputWidth int
	ArrowPad   int
	ViewAction string
	EditAction string
	AutoFocus  bool
	// OnActivate is called when the view-mode row is activated.
	// If nil, the row auto-switches to edit mode.
	OnActivate func()
	// OnSubmit is called when the edit-mode input submits.
	// The caller decides whether to Close or keep editing.
	OnSubmit func(string)
	editing  *tui.State[bool]
}

// NewEditableRow builds a mountable editable row for a single text field.
func NewEditableRow(id, label string, value *tui.State[string]) *EditableRow {
	return &EditableRow{
		SelectBase: NewSelectBase(id),
		Label:      label,
		Value:      value,
		InputWidth: ActionRowValueWidth,
		ViewAction: "edit ↵",
		EditAction: "save ↵",
		editing:    tui.NewState(false),
	}
}

// Open switches the row to edit mode. The mounted input traps focus via
// autoFocus on the next render.
func (r *EditableRow) Open() {
	r.editing.Set(true)
	// Seed focus so the next render cycle aligns the visible marker with the
	// newly mounted input before go-tui resolves its ref.
	r.focused.Set(true)
}

// Init seeds focus when the harness or app mounts this component with
// AutoFocus set, matching SelectableRow behavior.
func (r *EditableRow) Init() func() {
	if !r.AutoFocus {
		return nil
	}
	r.focused.Set(true)
	if r.app != nil {
		r.Focus(r.app)
	}
	return nil
}

// Close returns the row to view mode.
func (r *EditableRow) Close() {
	r.editing.Set(false)
}

// IsEditing returns true when the input is visible.
func (r *EditableRow) IsEditing() bool {
	return r.editing.Get()
}

func (r *EditableRow) BindApp(app *tui.App) {
	r.SelectBase.BindApp(app)
	if r.editing != nil {
		r.editing.BindApp(app)
	}
}

func (r *EditableRow) UnbindApp() {
	r.SelectBase.UnbindApp()
}

func (r *EditableRow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*EditableRow)
	if !ok {
		return
	}
	r.Label = f.Label
	r.Value = f.Value
	r.InputWidth = f.InputWidth
	r.ArrowPad = f.ArrowPad
	r.ViewAction = f.ViewAction
	r.EditAction = f.EditAction
	r.OnActivate = f.OnActivate
	r.OnSubmit = f.OnSubmit
}

func (r *EditableRow) Render(app *tui.App) *tui.Element {
	if r.IsEditing() {
		return r.renderEdit(app)
	}
	return r.renderView(app)
}

func (r *EditableRow) renderView(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
		tui.WithFocusable(true),
		tui.WithOnFocus(r.OnFocus),
		tui.WithOnBlur(r.OnBlur),
		tui.WithOnActivate(func() {
			if r.OnActivate != nil {
				r.OnActivate()
			} else {
				r.Open()
			}
		}),
	)
	aw := selectableRowArrowWidth + r.ArrowPad
	if aw < 1 {
		aw = 1
	}
	root.AddChild(tui.New(tui.WithText(r.Arrow()), tui.WithWidth(aw)))
	root.AddChild(tui.New(tui.WithText(r.Label), tui.WithWidth(selectableRowLabelWidth)))
	root.AddChild(tui.New(
		tui.WithText(r.Value.Get()),
		tui.WithWidth(ActionRowValueWidth),
		tui.WithWrap(false),
		tui.WithTruncate(true),
	))
	root.AddChild(tui.New(tui.WithWidth(selectableRowActionGapWidth)))
	root.AddChild(tui.New(tui.WithText(r.ViewAction)))
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return root
}

func (r *EditableRow) renderEdit(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
	)
	aw := selectableRowArrowWidth + r.ArrowPad
	if aw < 1 {
		aw = 1
	}
	root.AddChild(tui.New(tui.WithText(r.ArrowWithActiveDescendant(true)), tui.WithWidth(aw)))
	root.AddChild(tui.New(tui.WithText(r.Label), tui.WithWidth(selectableRowLabelWidth)))

	input := app.MountPersistent(r, 0, func() tui.Component {
		return tui.NewInput(
			tui.WithInputValue(r.Value),
			tui.WithInputAutoFocus(true),
			tui.WithInputOnSubmit(func(val string) {
				if r.OnSubmit != nil {
					r.OnSubmit(val)
				}
			}),
			tui.WithInputWidth(r.InputWidth),
		)
	})
	root.AddChild(input)
	root.AddChild(tui.New(tui.WithText(r.EditAction)))
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return root
}

func (r *EditableRow) KeyMap() tui.KeyMap {
	return r.WithTraversal(ActivateFocused(func(tui.KeyEvent) {
		if r.OnActivate != nil {
			r.OnActivate()
		} else {
			r.Open()
		}
	}))
}

var (
	_ tui.Component    = (*EditableRow)(nil)
	_ tui.KeyListener  = (*EditableRow)(nil)
	_ tui.PropsUpdater = (*EditableRow)(nil)
	_ tui.AppBinder    = (*EditableRow)(nil)
	_ tui.Initializer  = (*EditableRow)(nil)
)
