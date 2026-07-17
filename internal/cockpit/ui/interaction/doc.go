// Package interaction owns Cockpit's low-level go-tui interaction grammar.
//
// Product code should normally import internal/cockpit/ui, not this package.
// The parent ui package exposes ergonomic rows, pickers, editors, and browser
// controls. This package is the boundary where those controls are allowed to
// speak go-tui focus, refs, traversal, focus-gated key handlers, autofocus, and
// scroll-follow mechanics.
//
// The package exists to keep one focus authority. go-tui owns focus. Cockpit
// grammar may store stable IDs for mount keys, projection repair, and
// deterministic autofocus, but it must not maintain a second selected-row
// cursor while a mounted focusable element exists.
package interaction
