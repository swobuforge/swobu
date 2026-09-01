// Package routes renders Cockpit route rows and expanded target detail.
//
// Route parent rows own their local expand/collapse activation. Target child
// rows are rendered into the go-tui focus graph only while their parent route
// is expanded. This package does not sort, mutate, register rows upward, or
// redefine domain routing semantics. A zero-target route omits the inapplicable
// default row so add target is its next visible and selectable action.
// Onboarding route creation, rename, and delete remain local; the first target
// save crosses the existing atomic workspace-seed command and promotes the
// workspace from Cockpit projection to persisted state. Share is a route-local
// projection: issue uses the seven-day default, copy reveals just in time, and
// revoke reuses the existing confirmation grammar.
package routes
