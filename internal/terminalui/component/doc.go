// Package component owns the author-facing semantic component layer for
// terminalui.
//
// Components build core.Node[E] values, own build-scoped local state, and keep
// build-time dispatch/emit guarded. They do not own rendergraph internals,
// terminal I/O, or cockpit-specific truth.
package component
