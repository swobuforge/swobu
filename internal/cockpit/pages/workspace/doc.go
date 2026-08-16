// Package workspace renders the Cockpit page for one selected Swobu workspace.
//
// The page composes overview, routes, and activity sections for persisted and
// conventional-first workspaces. Bootstrap Activity renders its empty local
// state without querying the daemon. An unnamed ordinary draft renders only
// naming; a named draft also renders local route/target onboarding until the
// first target atomically persists the workspace. Conventional-first onboarding
// skips naming and renders endpoint, routes, and local empty Activity
// immediately. The workspace read model contains product projection data only;
// section expansion and detail state live in the section component that owns
// the interaction.
//
// Keyboard focus is framework state owned by go-tui. This package may request
// traversal or activation, but it must not maintain section-row ordering,
// selected-row references, or cursor state.
package workspace
