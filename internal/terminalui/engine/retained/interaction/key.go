package interaction

// Key is a typed keyboard input. The zero value means "no key" and should not
// appear in dispatched events.
type Key uint16

// Canonical navigation and control keys consumed by the runtime or views.
const (
	KeyNone Key = iota
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyEsc
	KeyEnter
	KeyTab
	KeyShiftTab
	KeyBackspace
	KeySpace
	KeyRune // carries the actual character in Event.Rune
)

// Modifiers holds the shift/alt/ctrl state for one key event.
type Modifiers uint8

const (
	ModShift Modifiers = 1 << iota
	ModAlt
	ModCtrl
)

// ParseKey translates a historical string key name into a typed Key enum. It exists
// only for migration compatibility; new code should use typed Key values directly.
// Unknown names fall back to KeyRune with the original rune preserved.
func ParseKey(name string, r rune) (Key, rune) {
	keyName := name // swobu:io-string source=terminal-input
	switch keyName {
	case "up":
		return KeyUp, 0
	case "down":
		return KeyDown, 0
	case "left":
		return KeyLeft, 0
	case "right":
		return KeyRight, 0
	case "esc":
		return KeyEsc, 0
	case "enter":
		return KeyEnter, 0
	case "tab":
		return KeyTab, 0
	case "shift+tab":
		return KeyShiftTab, 0
	case "backspace":
		return KeyBackspace, 0
	case "space":
		return KeySpace, 0
	case "rune":
		if r == 0 || r == '\n' || r == '\r' {
			return KeyNone, 0
		}
		return KeyRune, r
	default:
		// Unknown named key: treat as a rune if printable, otherwise ignore.
		if r >= 0x20 && r != 0x7f {
			return KeyRune, r
		}
		return KeyNone, 0
	}
}

// IsControl reports whether this key is a non-character control key.
func (k Key) IsControl() bool {
	return k > KeyNone && k < KeyRune
}

// String returns a stable diagnostic label for the key.
func (k Key) String() string {
	switch k {
	case KeyNone:
		return "none"
	case KeyUp:
		return "up"
	case KeyDown:
		return "down"
	case KeyLeft:
		return "left"
	case KeyRight:
		return "right"
	case KeyEsc:
		return "esc"
	case KeyEnter:
		return "enter"
	case KeyTab:
		return "tab"
	case KeyShiftTab:
		return "shift+tab"
	case KeyBackspace:
		return "backspace"
	case KeySpace:
		return "space"
	case KeyRune:
		return "rune"
	default:
		return "unknown"
	}
}
