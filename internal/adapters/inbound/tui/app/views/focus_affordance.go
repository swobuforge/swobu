package views

import (
	"strings"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
)

func FocusAffordance(verb string, allowSpace bool) func() []update.Action {
	cleanVerb := strings.TrimSpace(verb)
	return func() []update.Action {
		return []update.Action{state.SetFocusedRowAffordance{Verb: cleanVerb, AllowSpace: allowSpace}}
	}
}

func focusAffordance(verb string, allowSpace bool) func() []update.Action {
	return FocusAffordance(verb, allowSpace)
}
