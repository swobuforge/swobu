package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func FocusAffordance(verb string, allowSpace bool) func() []update.Action {
	cleanVerb := strings.TrimSpace(verb) // swobu:io-string source=boundary
	return func() []update.Action {
		return []update.Action{state.SetFocusedRowAffordance{Verb: cleanVerb, AllowSpace: allowSpace}}
	}
}

func focusAffordance(verb string, allowSpace bool) func() []update.Action {
	verb = strings.TrimSpace(verb) // swobu:io-string source=boundary
	if verb == "" {
		verb = "act"
	}
	return FocusAffordance(verb, allowSpace)
}
