package ui

import (
	"unicode/utf8"

	tui "github.com/grindlemire/go-tui"
)

// InlineEditor owns the visual text surface and typing keymap for a single
// inline text field. It is NOT a tui.Component and does not own an edit/view
// state. The caller decides when to render the surface and when to show the
// view-mode row.
//
// Use InlineEditor when you need inline editing inside a custom Component
// (e.g. a workflow with its own Phase state). If you just need a standard
// arrow-label-value-action row, use EditableRow instead.
//
// Rules:
//   - Focus never leaves the parent shell. The text surface is non-focusable.
//   - Escape and Enter are NOT bound here. The parent decides what they mean.
//   - Typing keys (Rune, Backspace, Delete, arrows, Home, End) are bound via
//     TypingKeyMap and forwarded into InlineInput.
//   - The parent must include the InlineEditor in BindApp and Watchers.
type InlineEditor struct {
	Value     *tui.State[string]
	Width     int
	Sensitive bool
	OnSubmit  func(string)
	OnChange  func(string)
	OnClose   func()

	input *InlineInput
}

// NewInlineEditor creates an InlineEditor backed by the given shared value.
func NewInlineEditor(value *tui.State[string]) *InlineEditor {
	return &InlineEditor{
		Value: value,
		Width: ActionRowValueWidth,
	}
}

// BindApp wires internal state to the app.
func (ed *InlineEditor) BindApp(app *tui.App) {
	if ed.input != nil {
		ed.input.BindApp(app)
	}
}

// Watchers returns the cursor-blink watcher.
func (ed *InlineEditor) Watchers() []tui.Watcher {
	if ed.input != nil {
		return ed.input.Watchers()
	}
	return nil
}

// Render returns the visual element. Safe to call even before Open (returns a
// zero-width element).
func (ed *InlineEditor) Render() *tui.Element {
	if ed.input == nil {
		return tui.New(tui.WithWidth(0))
	}
	ed.input.Sensitive = ed.Sensitive
	return ed.input.Render()
}

// TypingKeyMap returns the typing key bindings forwarded into InlineInput.
func (ed *InlineEditor) TypingKeyMap() tui.KeyMap {
	ed.ensureInput()
	return tui.KeyMap{
		tui.OnFocused(tui.KeyRune, func(e tui.KeyEvent) { ed.input.HandleKey(e) }),
		tui.OnFocused(tui.KeyBackspace, func(e tui.KeyEvent) { ed.input.HandleKey(e) }),
		tui.OnFocused(tui.KeyDelete, func(e tui.KeyEvent) { ed.input.HandleKey(e) }),
		tui.OnFocused(tui.KeyLeft, func(e tui.KeyEvent) { ed.input.HandleKey(e) }),
		tui.OnFocused(tui.KeyRight, func(e tui.KeyEvent) { ed.input.HandleKey(e) }),
		tui.OnFocused(tui.KeyHome, func(e tui.KeyEvent) { ed.input.HandleKey(e) }),
		tui.OnFocused(tui.KeyEnd, func(e tui.KeyEvent) { ed.input.HandleKey(e) }),
		tui.OnFocused(tui.KeyEnter, func(e tui.KeyEvent) { ed.input.HandleKey(e) }),
	}
}

// SetText seeds the InlineInput value and cursor state.
func (ed *InlineEditor) SetText(s string) {
	ed.ensureInput()
	ed.input.SetText(s)
}

// Close resets the visible surface. If OnClose is set it is called first.
func (ed *InlineEditor) Close() {
	if ed.OnClose != nil {
		ed.OnClose()
	}
}

func (ed *InlineEditor) ensureInput() {
	if ed.input != nil {
		return
	}
	ed.input = NewInlineInput(ed.Value)
	ed.input.Width = ed.Width
	ed.input.Sensitive = ed.Sensitive
	ed.input.OnSubmit = func(_ string) {
		if ed.OnSubmit != nil {
			ed.OnSubmit(ed.Value.Get())
		}
	}
	ed.input.OnChange = ed.OnChange
}

// CursorPos returns the current cursor position in runes.
func (ed *InlineEditor) CursorPos() int {
	if ed.input == nil {
		return utf8.RuneCountInString(ed.Value.Get())
	}
	return ed.input.CursorPos()
}
