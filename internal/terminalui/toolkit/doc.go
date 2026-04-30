// Package toolkit groups reusable batteries-included authoring helpers and
// views that sit above the retained engine and below any concrete TUI app.
//
// Toolkit packages own generic views and authoring helpers that another app
// could reuse without inheriting Swobu cockpit semantics. Cockpit shell
// composition, section order, status vocabulary, and operator-surface
// responsive policy belong in `internal/terminalui/apps/cockpit/app`.
package toolkit
