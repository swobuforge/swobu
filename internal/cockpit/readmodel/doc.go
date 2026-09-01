// Package readmodel defines the Cockpit-owned projection consumed by go-tui
// surfaces, sections, fixtures, and adapter ports.
//
// The package is UI contract, not domain truth. It carries stable operator
// identifiers, typed state, shell notices, and small derived-label helpers so
// Cockpit views do not scatter prose summaries or infer routing semantics from
// raw domain data. Activity rows preserve daemon observation labels as display
// text instead of pretending partial clock strings are real timestamps. Tiny
// identifier aliases live beside the read model that owns their noun; a one-
// line alias file is noise unless the identifier becomes a real boundary.
// Draft construction and reset also live here so clearing authored setup cannot
// accidentally discard ambient provider options supplied by the adapter. Share
// projection carries only hostname and expiry; bearer secrets never enter this
// package.
package readmodel
