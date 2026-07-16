// ui provides shared select-flow mechanics for cockpit.
//
// Components in pages, sections, and features keep their own rendering and
// behavior. They reuse this package for the thin grammar over go-tui focus:
// focus markers, focused-key activation, editable text-input rows with
// focus-trap handoff, and shared row-marker helpers for rows with active
// descendant controls.
//
// SelectableRow is the canonical selectable action row with label/value/action.
// Its value column is shared across action-bearing cockpit rows so verbs stay
// aligned even when feature packages render their own variants.
// EditableRow extends that pattern with an inline text-input phase; callers
// set the value state and submit callback, and the row owns the view↔edit
// transition and input focus trap.
//
// FocusTrap (focus_trap.go) is a separate workaround layer. go-tui does not
// expose direct focus-by-ref, so focus repair after render changes walks the
// focus ring via public traversal. It is honest about being a hack, not normal
// app logic.
//
// This package is not a universal row renderer and must not become a second UI
// framework. Function templates are valid for inert layout. Interactive
// selectable components must be mounted struct components with local KeyMap
// ownership and a single render path; app/non-app render forks are split-brain
// proof.
package ui
