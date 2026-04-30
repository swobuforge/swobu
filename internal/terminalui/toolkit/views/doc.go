// Package views defines generic batteries-included views built on top of
// the retained TUI engine.
//
// This package owns reusable views such as rows, guards, pickers, editors,
// and behavior-oriented primitives (`Action`, `Input`, `ChoiceList`).
// It does not own Swobu cockpit shell composition,
// canonical section order, header/footer semantics, or cockpit-specific
// responsive thresholds; those belong in `internal/terminalui/apps/cockpit/app`.
package views
