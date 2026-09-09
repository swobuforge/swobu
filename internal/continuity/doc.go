// Package continuity resolves durable conversation continuation state.
//
// Checkpoints and thread heads are partitioned by workspace. The memory store
// retains a bounded process-local window; expiry and restart discard it.
// Head advancement checks the expected current head before replacing it.
// Transport authentication and client encoding remain outside this package.
package continuity
