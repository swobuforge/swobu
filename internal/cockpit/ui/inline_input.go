package ui

import (
	"strings"
	"time"
	"unicode/utf8"

	tui "github.com/grindlemire/go-tui"
)

const (
	inlineInputCursorRune      = '_'
	inlineInputCursorBlinkRune = ' '
)

// InlineInput is a cockpit-managed single-line text display primitive.
//
// It is NOT a tui.Component. It does NOT receive focus. The parent Component
// owns focus, key events, and the edit lifecycle. InlineInput is merely the
// visual surface: it renders text, manages cursor position/scroll/blink, and
// exposes a KeyMap()-style handler that the parent can forward into.
//
// Text value is read from and written to the shared Value state. There is no
// separate internal text buffer, so the parent always sees a consistent value.
type InlineInput struct {
	Value           *tui.State[string]
	OnSubmit        func(string)
	OnChange        func(string)
	Width           int // zero means ActionRowValueWidth
	Placeholder     string
	Sensitive       bool
	CursorRune      rune
	CursorBlinkRune rune

	cursorPos *tui.State[int]
	scrollPos *tui.State[int]
	blink     *tui.State[bool]
}

// NewInlineInput creates an InlineInput backed by the given shared value state.
//
// When the operator types, InlineInput writes back into value. The caller
// should bind value to the app so downstream consumers receive dirty marks.
func NewInlineInput(value *tui.State[string]) *InlineInput {
	return &InlineInput{
		Value:           value,
		Width:           ActionRowValueWidth,
		CursorRune:      inlineInputCursorRune,
		CursorBlinkRune: inlineInputCursorBlinkRune,
		cursorPos:       tui.NewState(utf8.RuneCountInString(value.Get())),
		scrollPos:       tui.NewState(0),
		blink:           tui.NewState(true),
	}
}

// BindApp wires internal state into the app.
func (inp *InlineInput) BindApp(app *tui.App) {
	inp.cursorPos.BindApp(app)
	inp.scrollPos.BindApp(app)
	inp.blink.BindApp(app)
}

// UnbindApp releases app handles. Safe to call when the parent unmounts.
func (inp *InlineInput) UnbindApp() {
	// No-op: states do not hold app references after BindApp(nil) pattern.
}

// Watchers returns cursor-blink watchers. The parent Component should include
// these in its own Watchers() implementation so go-tui drives the blink.
func (inp *InlineInput) Watchers() []tui.Watcher {
	return []tui.Watcher{
		tui.OnTimer(500*time.Millisecond, func() {
			inp.blink.Set(!inp.blink.Get())
		}),
	}
}

// HandleKey routes keyboard input into the text surface.
//
// Returns true when the key was consumed (typing, navigation, submit).
// Returns false for unrecognised keys so the parent can delegate further.
//
// KeyEscape is NOT consumed here. The parent owns the Escape lifecycle.
func (inp *InlineInput) HandleKey(ke tui.KeyEvent) bool {
	switch ke.Key {
	case tui.KeyRune:
		inp.insertChar(ke)
		return true
	case tui.KeyBackspace:
		inp.backspace()
		return true
	case tui.KeyDelete:
		inp.delete()
		return true
	case tui.KeyLeft:
		inp.moveLeft()
		return true
	case tui.KeyRight:
		inp.moveRight()
		return true
	case tui.KeyHome:
		inp.moveHome()
		return true
	case tui.KeyEnd:
		inp.moveEnd()
		return true
	case tui.KeyEnter:
		inp.submit()
		return true
	}
	// KeyEscape and all other keys fall through so the parent can handle them.
	return false
}

// SetText overwrites the current text in Value, cursor moves to end.
func (inp *InlineInput) SetText(s string) {
	inp.Value.Set(s)
	inp.cursorPos.Set(utf8.RuneCountInString(s))
	inp.scrollPos.Set(0)
	inp.blink.Set(true)
}

// MoveHome places the cursor at the start of the current value and resets the
// viewport so destination-bearing prefixes remain visible when editing opens.
func (inp *InlineInput) MoveHome() {
	inp.moveHome()
}

// CursorPos returns the current cursor position in runes.
func (inp *InlineInput) CursorPos() int {
	return inp.cursorPos.Get()
}

// Render returns the visual element for the current text + cursor state.
//
// The returned element is NOT focusable. It carries no onFocus/onBlur hooks.
// The parent should place it as a child of the row element, which IS focusable.
func (inp *InlineInput) Render() *tui.Element {
	text := inp.Value.Get()
	if text == "" && inp.Placeholder != "" {
		text = inp.Placeholder
	} else if inp.Sensitive {
		text = strings.Repeat("•", utf8.RuneCountInString(text))
	}

	display := inp.displayText(text)

	return tui.New(
		tui.WithText(display),
		tui.WithWidth(inp.width()),
		tui.WithWrap(false),
		tui.WithTruncate(false),
	)
}

// --- internal text operations ---

func (inp *InlineInput) insertChar(ke tui.KeyEvent) {
	runes := []rune(inp.Value.Get())
	pos := inp.clampCursorPos()
	newRunes := make([]rune, 0, len(runes)+1)
	newRunes = append(newRunes, runes[:pos]...)
	newRunes = append(newRunes, ke.Rune)
	newRunes = append(newRunes, runes[pos:]...)
	inp.Value.Set(string(newRunes))
	inp.cursorPos.Set(pos + 1)
	inp.blink.Set(true)
	inp.ensureCursorVisible()
	if inp.OnChange != nil {
		inp.OnChange(inp.Value.Get())
	}
}

func (inp *InlineInput) backspace() {
	runes := []rune(inp.Value.Get())
	pos := inp.clampCursorPos()
	if pos > 0 {
		newRunes := append(runes[:pos-1], runes[pos:]...)
		inp.Value.Set(string(newRunes))
		newPos := pos - 1
		inp.cursorPos.Set(newPos)
		inp.adjustScrollBackspace(newRunes, newPos)
		if inp.OnChange != nil {
			inp.OnChange(inp.Value.Get())
		}
	}
}

func (inp *InlineInput) delete() {
	runes := []rune(inp.Value.Get())
	pos := inp.clampCursorPos()
	if pos < len(runes) {
		newRunes := append(runes[:pos], runes[pos+1:]...)
		inp.Value.Set(string(newRunes))
		inp.adjustScrollDelete(newRunes, pos)
		if inp.OnChange != nil {
			inp.OnChange(inp.Value.Get())
		}
	}
}

func (inp *InlineInput) moveLeft() {
	pos := inp.cursorPos.Get()
	if pos > 0 {
		inp.cursorPos.Set(pos - 1)
		inp.blink.Set(true)
		inp.ensureCursorVisible()
	}
}

func (inp *InlineInput) moveRight() {
	if pos := inp.cursorPos.Get(); pos < utf8.RuneCountInString(inp.Value.Get()) {
		inp.cursorPos.Set(pos + 1)
		inp.blink.Set(true)
		inp.ensureCursorVisible()
	}
}

func (inp *InlineInput) moveHome() {
	inp.cursorPos.Set(0)
	inp.blink.Set(true)
	inp.ensureCursorVisible()
}

func (inp *InlineInput) moveEnd() {
	inp.cursorPos.Set(utf8.RuneCountInString(inp.Value.Get()))
	inp.blink.Set(true)
	inp.ensureCursorVisible()
}

func (inp *InlineInput) submit() {
	if inp.OnSubmit != nil {
		inp.OnSubmit(inp.Value.Get())
	}
}

// --- display ---

func (inp *InlineInput) displayText(original string) string {
	runes := []rune(original)
	pos := inp.clampCursorPos()
	visible := inp.visibleWidth()

	inp.ensureCursorVisible()
	scroll := inp.scrollPos.Get()
	cursor := inp.cursorRune()
	if !inp.blink.Get() {
		cursor = inp.cursorBlinkRune()
	}

	withCursor := make([]rune, 0, len(runes)+1)
	withCursor = append(withCursor, runes[:pos]...)
	withCursor = append(withCursor, cursor)
	withCursor = append(withCursor, runes[pos:]...)

	viewStart := scroll
	if scroll > pos {
		viewStart = scroll + 1
	}
	viewEnd := min(viewStart+visible+1, len(withCursor))
	if viewEnd < viewStart {
		viewEnd = viewStart
	}
	return string(withCursor[viewStart:viewEnd])
}

func (inp *InlineInput) ensureCursorVisible() {
	pos := inp.clampCursorPos()
	visible := inp.visibleWidth()
	scroll := inp.scrollPos.Get()

	if pos < scroll {
		inp.scrollPos.Set(pos)
	} else if pos >= scroll+visible {
		inp.scrollPos.Set(pos - visible + 1)
	}
}

func (inp *InlineInput) adjustScrollBackspace(newRunes []rune, newPos int) {
	visible := inp.visibleWidth()
	if len(newRunes) >= visible && newPos >= visible {
		inp.scrollPos.Set(newPos - visible + 1)
	} else {
		inp.scrollPos.Set(0)
	}
}

func (inp *InlineInput) adjustScrollDelete(newRunes []rune, pos int) {
	visible := inp.visibleWidth()
	if len(newRunes) >= visible && pos >= visible {
		inp.scrollPos.Set(pos - visible + 1)
	} else {
		inp.scrollPos.Set(0)
	}
}

func (inp *InlineInput) clampCursorPos() int {
	pos := inp.cursorPos.Get()
	if pos < 0 {
		return 0
	}
	max := utf8.RuneCountInString(inp.Value.Get())
	if pos > max {
		return max
	}
	return pos
}

func (inp *InlineInput) visibleWidth() int {
	if inp.Width > 0 {
		return inp.Width
	}
	return ActionRowValueWidth
}

func (inp *InlineInput) width() int {
	return inp.visibleWidth()
}

func (inp *InlineInput) cursorRune() rune {
	if inp.CursorRune != 0 {
		return inp.CursorRune
	}
	return inlineInputCursorRune
}

func (inp *InlineInput) cursorBlinkRune() rune {
	if inp.CursorBlinkRune != 0 {
		return inp.CursorBlinkRune
	}
	return inlineInputCursorBlinkRune
}
