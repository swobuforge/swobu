// Package compatibility owns Swobu's semantic center for the v0 request path.
//
// It classifies ingress families, normalizes paths, preserves canonical request,
// output, and continuation semantics, and defines the Swobu-vs-backend error
// boundary. For continuation-aware responses flows it owns authoritative thread
// meaning, anchored last-turn derivation, chain-aware prefix preparation inside
// an endpoint namespace, and the narrow load/capture contract.
// Canonical outputs may also carry provider-neutral token usage and cache
// accounting so adapters can expose runtime cost facts without provider-dialect
// leakage into core nouns.
// Provider-specific DTOs, realized wire payloads, and transport mechanics must
// stay outside this package.
package compatibility
