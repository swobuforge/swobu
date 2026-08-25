package ui

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui/interaction"
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
// activation. Typed text stays in a private draft and is published to Value
// only on submit, so dependent UI cannot react to a half-entered field. Focus
// never leaves the row shell: the text surface is rendered
// by an owned InlineEditor that is itself NOT a Component. The row Component
// owns the edit/view state (the InlineEditor is stateless about edit/view),
// typing keys (via InlineEditor.TypingKeyMap), the Escape lifecycle, and the
// optional validation taxonomy used by create-mode fields.
type EditableRow struct {
	target      *interaction.Selectable
	Label       string
	Value       *tui.State[string]
	Placeholder string
	// Sensitive masks the edit surface without changing the underlying value.
	Sensitive  bool
	ViewValue  func(string) string
	ValueWidth int // zero means ActionRowValueWidth
	ViewAction string
	EditAction string
	Validation EditableRowValidation
	// ValidationText is caller-owned helper copy for validation states.
	// EditableRow owns taxonomy and row layout; feature workflows own field
	// meaning such as workspace-name guidance or backend conflict text.
	ValidationText string
	// AutoFocus seeds the shell as selected on mount, or on the first
	// transition from false to true on an already-mounted row.
	AutoFocus bool
	// StartEditing seeds the row as already entered when it first mounts, or
	// when a fresh mount asks an existing row to enter editing.
	StartEditing bool
	// PublishWhileEditing mirrors the private edit draft into Value on each
	// keystroke. Use only when Value is itself control-local draft state and
	// live validation must react while typing.
	PublishWhileEditing bool
	// OpenAtStart keeps a destination-bearing prefix visible when a long value
	// first enters edit mode. Ordinary text fields retain cursor-at-end editing.
	OpenAtStart bool
	// OnActivate is called when the view-mode row is activated.
	// If nil, the row auto-switches to edit mode.
	OnActivate func()
	// OnSubmit is called when the edit-mode input submits.
	// The caller decides whether to Close or keep editing.
	OnSubmit func(string)
	// CloseAfterSubmit lets the mounted row close itself after a caller-owned
	// submit succeeds. Use this when a render-built callback cannot safely hold
	// the mounted row instance.
	CloseAfterSubmit func() bool
	// OnClose runs when Escape cancels the edit-mode shell.
	OnClose func()

	editing   *tui.State[bool]
	editValue *tui.State[string]
	editor    *InlineEditor
}

// NewEditableRow builds a mountable editable row for a single text field.
func NewEditableRow(id, label string, value *tui.State[string]) *EditableRow {
	editValue := tui.NewState("")
	row := &EditableRow{
		Label:      label,
		Value:      value,
		ValueWidth: ActionRowValueWidth,
		ViewAction: "edit ↵",
		EditAction: "save ↵",
		editing:    tui.NewState(false),
		editValue:  editValue,
		editor:     NewInlineEditor(editValue),
	}
	row.target = interaction.NewSelectable(row.propsWithID(id))
	return row
}

// Open switches the row to edit mode. The row remains the focus target; the
// InlineEditor surface is rendered but is not focusable.
func (r *EditableRow) Open() {
	r.editing.Set(true)
	r.target.FocusedState().Set(true)
	r.editor.Width = r.rowWidth()
	seed := r.Value.Get()
	if r.PublishWhileEditing {
		r.editor.Value = r.Value
	} else {
		r.editValue.Set(seed)
		r.editor.Value = r.editValue
	}
	r.editor.input = nil
	r.editor.OnChange = func(value string) {
		if r.PublishWhileEditing {
			r.Value.Set(value)
		}
	}
	r.editor.SetText(seed)
	if r.OpenAtStart {
		r.editor.MoveHome()
	}
	r.editor.input.OnChange = r.editor.OnChange
	if app := r.target.App(); app != nil {
		r.editor.BindApp(app)
	}
	r.target.Focus(r.target.App())
	// Wire submit dynamically so the caller's callback is always current. Submit
	// the value forwarded by the InlineEditor (what the operator typed), not
	// r.Value: render reconciliation repoints r.Value to a fresh state seeded
	// from props, so it can lag behind the editor's live input mid-edit.
	r.editor.OnSubmit = func(submitted string) {
		r.Value.Set(submitted)
		if r.OnSubmit != nil {
			r.OnSubmit(submitted)
		}
		if r.CloseAfterSubmit != nil && r.CloseAfterSubmit() {
			r.Close()
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

// Init gives actual selection to a freshly mounted row that is declaratively
// auto-selected or already entered.
func (r *EditableRow) Init() func() {
	if r.StartEditing && !r.IsEditing() {
		r.Open()
		return nil
	}
	if !r.AutoFocus && !r.IsEditing() {
		return nil
	}
	// An entered editor is the keyboard owner, not merely a painted active
	// descendant. Reconcile actual selection on every fresh mount because
	// go-tui preserves focus by tree index and may otherwise select a sibling
	// that replaced the previously focused row.
	r.target.Focus(r.target.App())
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
	r.target.BindApp(app)
	r.editing.BindApp(app)
	r.editValue.BindApp(app)
	r.editor.BindApp(app)
}

func (r *EditableRow) UnbindApp() {
	r.target.UnbindApp()
}

func (r *EditableRow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*EditableRow)
	if !ok {
		return
	}
	prevStartEditing := r.StartEditing
	r.Label = f.Label
	r.Value = f.Value
	r.Placeholder = f.Placeholder
	r.Sensitive = f.Sensitive
	r.ViewValue = f.ViewValue
	r.ValueWidth = f.ValueWidth
	r.ViewAction = f.ViewAction
	r.EditAction = f.EditAction
	r.Validation = f.Validation
	r.ValidationText = f.ValidationText
	r.OnActivate = f.OnActivate
	r.OnSubmit = f.OnSubmit
	r.CloseAfterSubmit = f.CloseAfterSubmit
	r.OnClose = f.OnClose
	r.AutoFocus = f.AutoFocus
	r.StartEditing = f.StartEditing
	r.PublishWhileEditing = f.PublishWhileEditing
	r.OpenAtStart = f.OpenAtStart

	r.target.Update(r.props())
	if !prevStartEditing && r.StartEditing && !r.IsEditing() {
		r.Open()
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
	opts := append(r.shellOptions(), tui.WithOnActivate(func() {
		if r.OnActivate != nil {
			r.OnActivate()
		} else {
			r.Open()
		}
	}))
	root := ActionRow(r.Arrow(), r.Label, r.viewValue(), r.ActionLabel(), opts...)
	r.target.BindElement(root)
	return r.wrapWithHint(root)
}

func (r *EditableRow) viewValue() string {
	value := r.Value.Get()
	if r.ViewValue != nil {
		value = r.ViewValue(value)
	}
	if strings.TrimSpace(value) == "" && strings.TrimSpace(r.Placeholder) != "" {
		return r.Placeholder
	}
	return value
}

func (r *EditableRow) renderEdit() *tui.Element {
	r.editor.Width = r.rowWidth()
	r.editor.Sensitive = r.Sensitive
	root := EditRow(
		r.Arrow(),
		r.Label,
		r.editor.Render(),
		r.ActionLabel(),
		r.shellOptions()...,
	)
	r.target.BindElement(root)
	return r.wrapWithHint(root)
}

// Arrow returns the shared marker for the row's current visibility state.
// Edit mode counts as active even while the inline input owns the interaction.
func (r *EditableRow) Arrow() string {
	return r.target.MarkerWithActiveDescendant(r.IsEditing())
}

func (r *EditableRow) KeyMap() tui.KeyMap {
	if r.IsEditing() {
		km := tui.KeyMap{
			tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) {
				r.Cancel()
			}),
			tui.OnFocused(tui.KeyDown, func(tui.KeyEvent) {}),
			tui.OnFocused(tui.KeyUp, func(tui.KeyEvent) {}),
			tui.OnFocused(tui.AnyRune, func(e tui.KeyEvent) { r.editor.input.HandleKey(e) }),
			tui.OnFocused(tui.KeyBackspace, func(e tui.KeyEvent) { r.editor.input.HandleKey(e) }),
			tui.OnFocused(tui.KeyDelete, func(e tui.KeyEvent) { r.editor.input.HandleKey(e) }),
			tui.OnFocused(tui.KeyLeft, func(e tui.KeyEvent) { r.editor.input.HandleKey(e) }),
			tui.OnFocused(tui.KeyRight, func(e tui.KeyEvent) { r.editor.input.HandleKey(e) }),
			tui.OnFocused(tui.KeyHome, func(e tui.KeyEvent) { r.editor.input.HandleKey(e) }),
			tui.OnFocused(tui.KeyEnd, func(e tui.KeyEvent) { r.editor.input.HandleKey(e) }),
			tui.OnFocused(tui.KeyEnter, func(e tui.KeyEvent) { r.editor.input.HandleKey(e) }),
		}
		return km
	}
	return r.target.KeyMap()
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

// HelperText returns the optional caller-owned helper line that follows the row.
func (r *EditableRow) HelperText() string {
	return strings.TrimSpace(r.ValidationText)
}

func (r *EditableRow) wrapWithHint(root *tui.Element) *tui.Element {
	wrapper := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)
	wrapper.AddChild(root)
	if hint := r.HelperText(); hint != "" {
		wrapper.AddChild(editableRowHelperLine(hint))
	}
	return wrapper
}

func editableRowHelperLine(text string) *tui.Element {
	block := tui.New(
		tui.WithWidthPercent(100),
		tui.WithPaddingTRBL(0, 0, 0, 20),
	)
	block.AddChild(FlowText(text).Root)
	return block
}

func (r *EditableRow) rowWidth() int {
	if r.ValueWidth > 0 {
		return r.ValueWidth
	}
	return ActionRowValueWidth
}

func (r *EditableRow) IsFocused() bool { return r.target.IsFocused() }

func (r *EditableRow) Focus(app *tui.App) { r.target.Focus(app) }

func (r *EditableRow) Ref() *tui.Ref { return r.target.Ref() }

func (r *EditableRow) shellOptions() []tui.Option {
	props := r.props()
	props.AutoFocus = r.AutoFocus || r.IsEditing()
	r.target.SetRenderProps(props)
	return r.target.ShellOptions()
}

func (r *EditableRow) props() interaction.SelectableProps {
	return r.propsWithID(r.target.Props().ID)
}

func (r *EditableRow) propsWithID(id string) interaction.SelectableProps {
	return interaction.SelectableProps{
		ID:        id,
		Label:     r.Label,
		AutoFocus: r.AutoFocus || r.IsEditing(),
		OnActivate: func(interaction.Context) {
			if r.OnActivate != nil {
				r.OnActivate()
			} else {
				r.Open()
			}
		},
	}
}

var (
	_ tui.Component       = (*EditableRow)(nil)
	_ tui.KeyListener     = (*EditableRow)(nil)
	_ tui.PropsUpdater    = (*EditableRow)(nil)
	_ tui.AppBinder       = (*EditableRow)(nil)
	_ tui.Initializer     = (*EditableRow)(nil)
	_ tui.WatcherProvider = (*EditableRow)(nil)
)
