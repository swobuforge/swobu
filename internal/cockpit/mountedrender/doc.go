// Package mountedrender renders go-tui components through a seeded App without
// entering a real terminal.
//
// Cockpit components that own KeyMap or focus behavior must be rendered as
// mounted components. Calling Render(nil) on those components creates a second
// render path where generated app.Mount calls cannot run, which in turn pushes
// the codebase toward app/non-app split brain.
//
// The seam is intentionally small and framework-specific. go-tui v0.17.0 does
// not expose a public TTY-free App constructor, so this package seeds the
// private App fields required for mounted render and dispatch proof.
package mountedrender
