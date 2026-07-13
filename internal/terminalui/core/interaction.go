package core

// Intent is the semantic meaning of one key binding or action binding.
type Intent string

const (
	IntentActivate   Intent = "activate"
	IntentCancel     Intent = "cancel"
	IntentCopy       Intent = "copy"
	IntentEdit       Intent = "edit"
	IntentMoveNext   Intent = "move.next"
	IntentMovePrev   Intent = "move.prev"
	IntentBackspace  Intent = "backspace"
	IntentInsertRune Intent = "insert.rune"
)

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

// InteractionSpec[E] is the semantic interaction envelope for one node.
type InteractionSpec[E any] struct {
	Focus        FocusSpec
	Keymap       []KeyBindingSpec
	Help         []HelpBindingSpec
	Signals      []SignalEvent[E]
	FocusSignals []SignalEvent[E]
}
