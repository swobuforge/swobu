// Package continuitystore contains concrete adapters for the
// ResponseContinuityStore port.
//
// The active v0 implementation is an in-memory recent replay window. If Swobu
// later needs durable restart continuity, a persistent variant can sit behind
// the same port without forcing the role-oriented package name to change.
package continuitystore
