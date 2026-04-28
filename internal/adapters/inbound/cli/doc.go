// Package cli implements the operator-facing entrypoint and daemon namespace.
//
// It owns argument parsing, interactive-vs-noninteractive dispatch,
// machine-readable status output, and graceful lifecycle command behavior. It
// does not own runtime truth, provider wiring, or domain mutation semantics.
package cli
