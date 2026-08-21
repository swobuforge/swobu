// Package providers owns the outbound provider namespace registry and dispatch.
//
// It explicitly composes provider-local facets for manifest, discovery, and
// execution at the composition edge. Provider availability never depends on
// package initialization or mutable global registration.
//
// Provider-hosted web search is an exact-target semantic lowering. A protocol
// family does not grant hosted-search capability: a positively owned provider
// rule emits its spelling at the canonical occurrence, while an unowned target
// fails before transport. Target-aware does not currently mean model-aware:
// provider/protocol dialect rules are Swobu's current capability authority.
// Model IDs, catalog labels, backend prose, and inferred model families do not
// select lowering behavior. Pre-I/O incompatibility is guaranteed only for
// incompatibility known at Swobu's current capability granularity.
//
// An exact provider codec exists only for demonstrated non-default wire
// grammar and composes that spelling through protocolcodec before the
// protocol's single JSON serialization. Rules are occurrence-local; they do
// not mutate a completed request collection. This is typed encode-time
// composition, not serialized JSON mutation, provider capability prediction,
// hostname detection, or a dialect registry.
package providers
