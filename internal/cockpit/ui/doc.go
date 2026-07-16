// ui provides shared select-flow mechanics for cockpit.
//
// This package is not a universal UI framework. It contains five categories of
// reusable surface for a focused cockpit interaction model: focus markers,
// selectable rows, inline text editing primitives, static text lines, and
// focus repair.
//
// go-tui provides the focus graph. Cockpit layers selection, entered/leaved
// state, and typing semantics on top of that graph instead of inventing its
// own cursor system.
//
// The core ladder is:
//
//   SelectBase / SelectableRow / SectionDisclosure — selected shell and traversal
//   EditableRow                                     — selected shell plus inline text edit
//   SearchPicker                                    — selected shell plus bounded searchable choice
//   FocusableControl                                — explicit enter/exit scope
//
// Render stays pure. Autofocus seeds happen at mount/update boundaries only;
// render may read the declarative seed to draw the marker, but it must not
// reassert focus from Render or BindApp loops.
//
// ---------------------------------------------------------------------------
// Inline editing: ONE pattern
// ---------------------------------------------------------------------------
//
// Cockpit does NOT use go-tui's tui.Input. tui.Input mounts its own focusable
// element, which creates two focus failure classes we cannot tolerate:
//
//   1. KeyEscape is swallowed by the input element before the parent row can
//      act, because go-tui dispatches in BFS order (ancestors first) but the
//      input also binds OnFocused(KeyEscape) and the framework does not allow
//      bubbling.
//   2. When the input element is removed from the tree without receiving Blur(),
//      go-tui's refreshFromTree forgets to clear its focused state, leaving a
//      stale cursor on the next render.
//
// The cockpit replacement is a two-layer abstraction:
//
//   InlineInput  — visual text surface only. NOT a Component. NOT focusable.
//                  Manages cursor position, scroll, and blink. Never used
//                  directly by features/sections/pages.
//
//   InlineEditor — owns the InlineInput surface and the typing keymap.
//                  NOT a Component. The parent Composite owns edit/view state.
//                  Use this when the row is part of a larger Component that has
//                  its own Phase/Mode state (e.g. workspace_edit.Workflow,
//                  route_edit routeModelRowView).
//
//   EditableRow  — a Component that IS a selectable row with inline editing.
//                  Use this when you only need a standard
//                  arrow-label-value-action row that toggles into edit mode.
//                  It wraps InlineEditor internally, owns the edit/view state
//                  itself, and can project a small validation taxonomy for
//                  create-mode rows (`required`, `invalid`, `duplicate`) plus
//                  shared helper copy.
//
//   TextComponent — a minimal static text component for shared headers and
//                  helper lines that still need to participate in templ
//                  mounting.
//
// TOOWTDI: if you need a standard selectable row with inline text, use
// EditableRow. If you have custom lifecycle or layout that doesn't fit,
// compose InlineEditor into your own Component. Do NOT reach for tui.Input,
// tui.NewInput, or InlineInput directly.
//
// Example (EditableRow, the common case):
//
//     row := ui.NewEditableRow("id", "label", valueState)
//     row.OnSubmit = func(s string) { ... }
//     row.Validation = ui.EditableRowValidationRequired
//     row.ValidationText = "enter a workspace slug"
//
// Example (InlineEditor, for custom Components):
//
//     type MyWorkflow struct { editor *ui.InlineEditor }
//     w.editor = ui.NewInlineEditor(w.Slug)
//     w.editor.OnSubmit = func(_ string) { w.Submit(...) }
//     // in KeyMap() when editing:
//     return append(EscapeBinding, w.editor.TypingKeyMap()...)
//     // in Render() when editing:
//     surface := ui.EditRow("▌", "label", w.editor.Render(), "save ↵")
//
// ---------------------------------------------------------------------------
// FocusableControl
// ---------------------------------------------------------------------------
//
// FocusableControl (focusable_control.go) is the canonical interaction
// primitive that owns the full lifecycle: Focus/Blur, Activate (Enter/Space),
// Enter (focus into interior), and Exit (Escape from open interior). Use it
// for modal workflows, inline editors, expandable rows, pickers, and any
// control that the operator can enter and must be able to exit.
//
// ---------------------------------------------------------------------------
// SelectableRow
// ---------------------------------------------------------------------------
//
// SelectableRow is the canonical selectable action row with label/value/action.
// Its value column is shared across action-bearing cockpit rows so verbs stay
// aligned even when feature packages render their own variants.
//
// ---------------------------------------------------------------------------
// FocusTraversal
// ---------------------------------------------------------------------------
//
// FocusTraversal (focus_traversal.go) is a separate workaround layer. go-tui
// does not expose direct focus-by-ref, so focus repair after a mount/update
// transition walks the focus ring via public traversal. Use it once to repair
// a declarative autofocus transition; do not turn it into a render loop.
//
// ---------------------------------------------------------------------------
// Package boundary
// ---------------------------------------------------------------------------
//
// This package must not become a second UI framework. Function templates are
// valid for inert layout. Interactive selectable components must be mounted
// struct components with local KeyMap ownership and a single render path;
// app/non-app render forks are split-brain proof.
package ui
