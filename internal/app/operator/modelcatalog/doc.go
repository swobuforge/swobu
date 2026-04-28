// Package modelcatalog owns daemon-side operator queries for model-catalog read
// models.
//
// It composes endpoint intent with provider model-catalog ports to return one
// operator-facing snapshot. It does not own provider protocol behavior or TUI
// rendering.
package modelcatalog
