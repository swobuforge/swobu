// Package routes renders Cockpit route rows and expanded target detail.
//
// Route parent rows own their local expand/collapse activation. Target child
// rows are rendered into the go-tui focus graph only while their parent route
// is expanded. This package does not sort, mutate, register rows upward, or
// redefine domain routing semantics.
package routes
