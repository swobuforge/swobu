package ui

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
)

// EditableRowValidation classifies the shared text-input row states used by
// Cockpit create/edit flows.
type EditableRowValidation int

const (
	EditableRowValidationNone EditableRowValidation = iota
	EditableRowValidationRequired
	EditableRowValidationInvalid
	EditableRowValidationDuplicate
)

// EditableRow is a selectable row that reveals an inline text input on
// activation. Focus never leaves the row shell: the text surface is rendered
// by an owned InlineEditor that is itself NOT a Component. The row Component
// owns the edit/view state (the InlineEditor is stateless about edit/view),
// typing keys (via InlineEditor.TypingKeyMap), the Escape lifecycle, and the
// optional validation taxonomy used by create-mode fields.
type EditableRow struct {
	SelectBase
	Label      string
	Value      *tui.State[string]
	ValueWidth int // zero means ActionRowValueWidth
	ViewAction string
	EditAction string
	Validation EditableRowValidation
	// ValidationText overrides the default helper copy for validation states.
	// Use it for backend conflict text or any caller-specific guidance.
	ValidationText string
	// AutoFocus seeds the shell as selected on mount, or on the first
	// transition from false to true on an already-mounted row.
	AutoFocus  bool
	// OnActivate is called when the view-mode row is activated.
	// If nil, the row auto-switches to edit mode.
	OnActivate func()
	// OnSubmit is called when the edit-mode input submits.
	// The caller decides whether to Close or keep editing.
	OnSubmit func(string)
	// OnClose runs when Escape cancels the edit-mode shell.
	OnClose func()

	editing *tui.State[bool]
	editor  *InlineEditor
}

// NewEditableRow builds a mountable editable row for a single text field.
func NewEditableRow(id, label string, value *tui.State[string]) *EditableRow {
	return &EditableRow{
		SelectBase: NewSelectBase(id),
		Label:      label,
		Value:      value,
		ValueWidth: ActionRowValueWidth,
		ViewAction: "edit ↵",
		EditAction: "save ↵",
		editing:    tui.NewState(false),
		editor:     NewInlineEditor(value),
	}
}

// Open switches the row to edit mode. The row remains the focus target; the
// InlineEditor surface is rendered but is not focusable.
func (r *EditableRow) Open() {
	r.editing.Set(true)
	r.focused.Set(true)
	r.editor.Width = r.rowWidth()
	r.editor.SetText(r.Value.Get())
	// Wire submit dynamically so the caller's callback is always current.
	r.editor.OnSubmit = func(_ string) {
		if r.OnSubmit != nil {
			r.OnSubmit(r.Value.Get())
		}
	}
}

// Cancel closes the row and notifies the caller that the edit shell was
// dismissed. Escape should call this, while programmatic reseeds should use
// Close so they do not reapply cancellation side effects.
func (r *EditableRow) Cancel() {
	if r.OnClose != nil {
		r.OnClose()
	}
	r.Close()
}

// Init seeds focus when the harness or app mounts this component with
// AutoFocus set, matching SelectableRow behavior.
func (r *EditableRow) Init() func() {
	if !r.AutoFocus && !r.IsEditing() {
		return nil
	}
	if r.IsFocused() {
		return nil
	}
	r.focused.Set(true)
	if r.app != nil {
		r.Focus(r.app)
	}
	return nil
}

// Close returns the row to view mode. The InlineEditor surface simply stops
// rendering, so the cursor disappears naturally.
func (r *EditableRow) Close() {
	r.editing.Set(false)
}

// IsEditing returns true when the input surface is visible.
func (r *EditableRow) IsEditing() bool {
	return r.editing.Get()
}

// BindApp wires the component's state to the app, including the InlineEditor.
func (r *EditableRow) BindApp(app *tui.App) {
	r.SelectBase.BindApp(app)
	r.editing.BindApp(app)
	r.editor.BindApp(app)
}

func (r *EditableRow) UnbindApp() {
	r.SelectBase.UnbindApp()
}

func (r *EditableRow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*EditableRow)
	if !ok {
		return
	}
	prevAutoFocus := r.AutoFocus
	r.Label = f.Label
	r.Value = f.Value
	r.ValueWidth = f.ValueWidth
	r.ViewAction = f.ViewAction
	r.EditAction = f.EditAction
	r.Validation = f.Validation
	r.ValidationText = f.ValidationText
	r.OnActivate = f.OnActivate
	r.OnSubmit = f.OnSubmit
	r.OnClose = f.OnClose
	r.AutoFocus = f.AutoFocus

	if !prevAutoFocus && r.AutoFocus && !r.IsFocused() {
		r.Focus(r.App())
	}
}

// Watchers returns the InlineEditor blink watcher.
func (r *EditableRow) Watchers() []tui.Watcher {
	return r.editor.Watchers()
}

func (r *EditableRow) Render(app *tui.App) *tui.Element {
	if r.IsEditing() {
		return r.renderEdit()
	}
	return r.renderView()
}

func (r *EditableRow) renderView() *tui.Element {
	root := ActionRow(r.Arrow(), r.Label, r.Value.Get(), r.ActionLabel(),
		tui.WithFocusable(true),
		tui.WithAutoFocus(r.AutoFocus || r.IsEditing()),
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
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return r.wrapWithHint(root)
}

func (r *EditableRow) renderEdit() *tui.Element {
	r.editor.Width = r.rowWidth()
	root := EditRow(
		r.Arrow(),
		r.Label,
		r.editor.Render(),
		r.ActionLabel(),
		tui.WithFocusable(true),
		tui.WithAutoFocus(r.AutoFocus || r.IsEditing()),
		tui.WithOnFocus(r.OnFocus),
		tui.WithOnBlur(r.OnBlur),
	)
	if r.Ref != nil {
		r.Ref.Set(root)
	}
	return r.wrapWithHint(root)
}

// Arrow returns the shared marker for the row's current visibility state.
// Edit mode counts as active even while the inline input owns the interaction.
func (r *EditableRow) Arrow() string {
	return r.SelectBase.ArrowWithActiveDescendant(r.IsEditing())
}

func (r *EditableRow) KeyMap() tui.KeyMap {
	if r.IsEditing() {
		km := tui.KeyMap{
			tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) {
				r.Cancel()
			}),
		}
		return r.WithTraversal(append(km, r.editor.TypingKeyMap()...))
	}
	return r.WithTraversal(ActivateFocused(func(tui.KeyEvent) {
		if r.OnActivate != nil {
			r.OnActivate()
		} else {
			r.Open()
		}
	}))
}

// ActionLabel returns the shared action grammar for this row's current state.
func (r *EditableRow) ActionLabel() string {
	switch r.Validation {
	case EditableRowValidationRequired:
		return "required"
	case EditableRowValidationInvalid:
		return "invalid"
	case EditableRowValidationDuplicate:
		return "duplicate"
	}
	if r.IsEditing() {
		if r.EditAction != "" {
			return r.EditAction
		}
		return "save ↵"
	}
	if r.ViewAction != "" {
		return r.ViewAction
	}
	return "edit ↵"
}

// HelperText returns the optional helper line that follows the row.
func (r *EditableRow) HelperText() string {
	if msg := strings.TrimSpace(r.ValidationText); msg != "" {
		return msg
	}
	switch r.Validation {
	case EditableRowValidationRequired:
		return "enter a workspace slug"
	case EditableRowValidationInvalid:
		return "use lowercase letters, numbers, and hyphens"
	}
	return ""
}

func (r *EditableRow) wrapWithHint(root *tui.Element) *tui.Element {
	wrapper := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)
	wrapper.AddChild(root)
	if hint := r.HelperText(); hint != "" {
		wrapper.AddChild(NewTextComponent(hint).Render(nil))
	}
	return wrapper
}

func (r *EditableRow) rowWidth() int {
	if r.ValueWidth > 0 {
		return r.ValueWidth
	}
	return ActionRowValueWidth
}

var (
	_ tui.Component       = (*EditableRow)(nil)
	_ tui.KeyListener     = (*EditableRow)(nil)
	_ tui.PropsUpdater    = (*EditableRow)(nil)
	_ tui.AppBinder       = (*EditableRow)(nil)
	_ tui.Initializer     = (*EditableRow)(nil)
	_ tui.WatcherProvider = (*EditableRow)(nil)
)
