// Package views defines app-owned cockpit views built on top of generic
// toolkit batteries.
//
// App-facing composition is function-first and returns retained or transcript
// view specs through the approved adapters. Runtime materialization stays
// inside engine/view. New cockpit view code must not import rendergraph
// packages directly; that boundary is enforced by tests in this package.
// Core-backed cockpit rows should go through `retained.FromCore` or the
// package-local `CoreNodeAsRetained` bridge, not direct rendergraph imports.
//
// Test authoring law for this package family:
// see `testing-screen-assertions.md` for intent-first lane selection between
// declarative screen predicates and visual diff contracts.
package views
