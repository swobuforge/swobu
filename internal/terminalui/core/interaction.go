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

// KeyMatch is a semantic match token for keyboard input.
type KeyMatch struct {
	Name string
}

// KeyEnter returns the enter key pattern.
func KeyEnter() KeyMatch { return KeyMatch{Name: "enter"} }

// KeyEsc returns the escape key pattern.
func KeyEsc() KeyMatch { return KeyMatch{Name: "esc"} }

// KeyRune returns one rune-pattern binding.
func KeyRune(r rune) KeyMatch { return KeyMatch{Name: string(r)} }

// KeyBindingSpec maps one key pattern to an intent.
type KeyBindingSpec struct {
	Pattern KeyMatch
	Intent  Intent
}

// HelpBindingSpec describes one footer/help binding.
type HelpBindingSpec struct {
	Key   string
	Label string
}

// InteractionSpec is the semantic interaction envelope for one node.
type InteractionSpec struct {
	Focus   FocusSpec
	Keymap  []KeyBindingSpec
	Help    []HelpBindingSpec
	Signals []SignalEvent
	// FocusSignals are emitted when the lowered node gains focus.
	FocusSignals []SignalEvent
}
