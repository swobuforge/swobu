package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// FocusAffordance returns a zero-argument function that produces the action
// that sets the current row's footer affordance. It is used by retained row
// builders that compose update.Action slices.
func FocusAffordance(verb string, allowSpace bool) func() []update.Action {
	cleanVerb := strings.TrimSpace(verb) // swobu:io-string source=boundary
	return func() []update.Action {
		return []update.Action{state.SetFocusedRowAffordance{Verb: cleanVerb, AllowSpace: allowSpace}}
	}
}

// FocusAffordanceSignal returns the signal event for core.Node paths.
func FocusAffordanceSignal(verb string, allowSpace bool) core.SignalEvent[state.Action] {
	return core.SignalEvent[state.Action]{
		Kind:  cockpitRowFocusSignalKind,
		Event: state.SetFocusedRowAffordance{Verb: strings.TrimSpace(verb), AllowSpace: allowSpace},
	}
}

func focusAffordance(verb string, allowSpace bool) func() []update.Action {
	verb = strings.TrimSpace(verb) // swobu:io-string source=boundary
	if verb == "" {
		verb = "act"
	}
	return FocusAffordance(verb, allowSpace)
}
