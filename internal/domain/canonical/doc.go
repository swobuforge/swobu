// Package canonical owns Swobu's semantic center for the v0 request path.
//
// It classifies client families, normalizes paths, preserves canonical request,
// output, and continuation semantics, and defines the Swobu-vs-backend error
// boundary. For continuation-aware flows it owns authoritative thread
// meaning, anchored last-turn derivation, chain-aware prefix preparation inside
// an endpoint namespace, and the narrow load/capture contract.
// Canonical outputs may also carry provider-neutral token usage and cache
// accounting so adapters can expose runtime cost facts without provider-dialect
// leakage into core nouns.
// Canonical request cache intent remains minimal by law: only fields with at
// least one active provider consumer are allowed in this package surface.
// Protocol-edge DTOs, realized wire payloads, and transport mechanics must
// stay outside this package.
package canonical
