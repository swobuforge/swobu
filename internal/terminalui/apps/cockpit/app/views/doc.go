// Package views defines app-owned cockpit views built on top of generic
// toolkit batteries.
//
// App-facing composition is function-first and returns view.ViewSpec[Model].
// Runtime materialization stays inside engine/view.
//
// Test authoring law for this package family:
// see `testing-screen-assertions.md` for intent-first lane selection between
// declarative screen predicates and visual diff contracts.
package views
