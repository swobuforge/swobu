// Package canonical owns Swobu's semantic center for the v0 request path.
//
// It classifies client families, normalizes paths, preserves canonical
// request/output/continuation semantics, and defines the Swobu-vs-backend
// error boundary. Continuation-aware flows use TurnRef and ContinuationRecord
// so semantic chain ownership stays separate from provider-native turn-state
// bytes. Canonical requests now also own the semantic tool ontology:
// function/capability declarations, execution ownership, and tool policy are
// represented separately so adapters can lower to wire formats without
// collapsing distinct lifecycles into one flat tool blob.
// Canonical outputs may also carry provider-neutral token usage and cache
// accounting so adapters can expose runtime cost facts without provider-
// dialect leakage into core nouns.
// Canonical request cache intent remains minimal by law: only fields with at
// least one active provider consumer are allowed in this package surface.
// Protocol-edge DTOs, realized wire payloads, and transport mechanics must
// stay outside this package.
package canonical
