// Package bootstrap wires the local Swobu runtime.
//
// It owns startup composition for the daemon process boundary: config loading,
// dependency wiring, listener startup, and the minimal runtime status truth the
// rest of the process can query. It does not own HTTP rendering, domain rules,
// or provider dialect behavior.
package bootstrap
