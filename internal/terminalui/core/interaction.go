package core

// Intent is the semantic meaning of one key binding or action binding.
type Intent string

const (
	IntentActivate Intent = "activate"
	IntentCancel   Intent = "cancel"
	IntentCopy     Intent = "copy"
	IntentEdit     Intent = "edit"
	IntentMoveNext Intent = "move.next"
	IntentMovePrev Intent = "move.prev"
)

// FocusMode describes how a node participates in focus.
type FocusMode uint8

const (
	FocusNone FocusMode = iota
	Focusable
	FocusGroup
	FocusScope
)

// FocusSpec is one node's focus contract.
type FocusSpec struct {
	Mode FocusMode
	Trap bool
}

// KeyPattern is a semantic match token for keyboard input.
type KeyPattern struct {
	Name string
}

// KeyEnter returns the enter key pattern.
func KeyEnter() KeyPattern { return KeyPattern{Name: "enter"} }

// KeyEsc returns the escape key pattern.
func KeyEsc() KeyPattern { return KeyPattern{Name: "esc"} }

// KeyRune returns one rune-pattern binding.
func KeyRune(r rune) KeyPattern { return KeyPattern{Name: string(r)} }

// KeyBinding maps one key pattern to an intent.
type KeyBinding struct {
	Pattern KeyPattern
	Intent  Intent
}

// HelpBinding describes one footer/help binding.
type HelpBinding struct {
	Key   string
	Label string
}

// Interaction is the semantic interaction envelope for one node.
type Interaction struct {
	Focus   FocusSpec
	Keymap  []KeyBinding
	Help    []HelpBinding
	Signals []Signal
	// FocusSignals are emitted when the lowered node gains focus.
	FocusSignals []Signal
}
