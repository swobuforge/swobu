package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// DisclosureNoteRows renders subordinate disclosure copy with wrapping so long
// backend messages do not break row grammar alignment.
func DisclosureNoteRows(note string) []retained.ViewSpec[state.Model] {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil
	}
	return wrappedPayloadTextRows("-> " + note)
}

// WrappedDetailRows renders wrapped subordinate copy without a leading marker.
func WrappedDetailRows(note string) []retained.ViewSpec[state.Model] {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil
	}
	return wrappedPayloadTextRows(note)
}
