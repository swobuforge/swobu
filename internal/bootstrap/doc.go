// Package bootstrap wires the local Swobu runtime.
//
// It owns startup composition for the daemon process boundary: config loading,
// dependency wiring, listener startup, and the minimal runtime status truth the
// rest of the process can query.
//
// Bootstrap also composes embedded telemetry runtime export as a downstream
// consumer of runtime evidence append events (plus local install state) rather
// than a request-path control input. It does not own HTTP rendering, domain
// rules, or provider dialect behavior.
package bootstrap
