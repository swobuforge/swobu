// ui provides shared selection and entry primitives for cockpit.
//
// This package is not a universal UI framework. It contains five categories of
// reusable surface for a focused cockpit interaction model: product-facing
// rows, disclosures, inline text editing primitives, choice/file controls,
// static text lines, and viewport follow.
//
// go-tui provides the focus graph. Cockpit application code speaks semantic
// selection: Up/Down selects an operator target, Enter/Space activates it, and
// Escape backs out of the nearest entered state. This package hides the focus
// graph behind row, picker, editor, and backout-scope primitives instead of
// inventing a custom cursor system.
//
// The core ladder is:
//
//	SelectableRow / SectionDisclosure — selected shell and activation
//	EditableRow                       — selected shell plus inline text edit
//	SearchPicker / FileBrowser        — domain wrappers over ChoiceList
//	ChoiceList / ChoiceRow            — one searchable/clipped list runtime
//	ActionTarget                      — custom shell behavior carrier
//	interaction                       — low-level focus/key/traversal grammar
//
// Picker/file menus do not own private operator cursors. ChoiceList declares
// selectable descendant rows and lets the root viewport follow the same
// selection cursor used by the rest of Cockpit. Its projection state is
// viewport state, not an independent selection model.
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
//  1. KeyEscape is swallowed by the input element before the parent row can
//     act, because go-tui dispatches in BFS order (ancestors first) but the
//     input also binds OnFocused(KeyEscape) and the framework does not allow
//     bubbling.
//  2. When the input element is removed from the tree without receiving Blur(),
//     go-tui's refreshFromTree forgets to clear its focused state, leaving a
//     stale cursor on the next render.
//
// The cockpit replacement is a two-layer abstraction:
//
//	InlineInput  — visual text surface only. NOT a Component. NOT focusable.
//	               Manages cursor position, scroll, and blink. Never used
//	               directly by features/sections/pages.
//
//	InlineEditor — owns the InlineInput surface and the typing keymap.
//	               NOT a Component. The parent Composite owns edit/view state.
//	               Use this when the row is part of a larger Component that has
//	               its own Phase/Mode state (e.g. workspace_edit.Workflow).
//
//	EditableRow  — a Component that IS a selectable row with inline editing.
//	               Use this when you only need a standard
//	               arrow-label-value-action row that toggles into edit mode.
//	               It wraps InlineEditor internally, owns the edit/view state
//	               itself, and can project a small validation taxonomy for
//	               create-mode rows (`required`, `invalid`, `duplicate`) plus
//	               caller-owned helper copy aligned under the value column.
//
// TOOWTDI: if you need a standard selectable row with inline text, use
// EditableRow. If you have custom lifecycle or layout that doesn't fit,
// compose InlineEditor into your own Component. Do NOT reach for tui.Input,
// tui.NewInput, or InlineInput directly.
//
// Example (EditableRow, the common case):
//
//	row := ui.NewEditableRow("id", "label", valueState)
//	row.OnSubmit = func(s string) { ... }
//	row.Validation = ui.EditableRowValidationRequired
//	row.ValidationText = "enter a workspace name"
//
// Example (InlineEditor, for custom Components):
//
//	type MyWorkflow struct { editor *ui.InlineEditor }
//	w.editor = ui.NewInlineEditor(w.Slug)
//	w.editor.OnSubmit = func(_ string) { w.Submit(...) }
//	// in KeyMap() when editing:
//	return append(EscapeBinding, w.editor.TypingKeyMap()...)
//	// in Render() when editing:
//	surface := ui.EditRow("_", "label", w.editor.Render(), "save ↵")
//
// ---------------------------------------------------------------------------
// Interaction Grammar
// ---------------------------------------------------------------------------
//
// The child interaction package is the low-level owner for focus cells,
// selectable targets, disclosure behavior, choice lists, file choosers, and
// viewport follow mechanics. New reusable controls should build on that grammar
// instead of exposing low-level go-tui focus/ref/traversal behavior.
//
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
// Focus traversal is owned by the interaction package. go-tui does not expose
// direct focus-by-ref, so mount/update focus repair walks the focus ring behind
// product-facing primitives. Page, section, and feature code must not own that
// workaround.
//
// ---------------------------------------------------------------------------
// Text Geometry & FlowText
// ---------------------------------------------------------------------------
//
// Cockpit text has three canonical geometries:
//  1. Single-line row value: nowrap + truncate in horizontal flex rows.
//  2. Flowing text: lossless wrapping + variable height in block layout (FlowText).
//  3. Editable input: single-line horizontal text entry viewport (InlineEditor).
//
// FlowText is the sole primitive for lossless flowing text (URLs, raw errors,
// multi-line instructions, file paths). It configures go-tui with WithWrap(true),
// WithTruncate(false), WithMinWidth(0), WithHeightAuto(), and WithWidthPercent(100).
// Invariant: FlowText must always be placed in a column/block container with
// layout-owned padding, never as a wrapping child of a horizontal flex row alongside
// fixed sibling elements.
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
