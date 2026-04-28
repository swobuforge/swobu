package views

import (
	"strings"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
)

// DisclosureNoteRows renders subordinate disclosure copy with wrapping so long
// backend messages do not break row grammar alignment.
func DisclosureNoteRows(note string) []view.ViewSpec[state.Model] {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil
	}
	return wrappedPayloadTextRows("-> " + note)
}
