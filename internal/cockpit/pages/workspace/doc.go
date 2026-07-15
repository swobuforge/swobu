// Package workspace renders the Cockpit page for one selected Swobu workspace.
//
// The page composes overview, routes, and activity sections. The workspace
// read model contains product projection data only; section expansion and
// detail state live in the section component that owns the interaction.
//
// Keyboard focus is framework state owned by go-tui. This package may request
// traversal or activation, but it must not maintain section-row ordering,
// selected-row references, or cursor state.
package workspace
