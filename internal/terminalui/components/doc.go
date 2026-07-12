// Package components groups reusable semantic component constructors that sit
// above the retained runtime and below any concrete cockpit view assembly.
//
// These helpers build core.Node values and intentionally avoid rendergraph and
// app/domain ownership. They are reusable authoring batteries, not runtime
// mechanics.
package components
