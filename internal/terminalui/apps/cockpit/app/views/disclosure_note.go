package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

const disclosureDetailWrapWidth = 50

// DisclosureNoteRows renders subordinate disclosure copy with wrapping so long
// backend messages do not break row grammar alignment.
func DisclosureNoteRows(note string) []retained.ViewSpec[state.Model] {
	note = strings.TrimSpace(note) // trimlowerlint:allow boundary canonicalization
	if note == "" {
		return nil
	}
	return toolkitviews.WrapLineRowsPreserveIndent("-> "+note, disclosureDetailWrapWidth, payloadTextRow)
}

// WrappedDetailRows renders wrapped subordinate copy without a leading marker.
func WrappedDetailRows(note string) []retained.ViewSpec[state.Model] {
	note = strings.TrimSpace(note) // trimlowerlint:allow boundary canonicalization
	if note == "" {
		return nil
	}
	return toolkitviews.WrapLineRowsPreserveIndent(note, disclosureDetailWrapWidth, payloadTextRow)
}
