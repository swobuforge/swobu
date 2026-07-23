// Package providers owns the outbound provider namespace registry and dispatch.
//
// It explicitly composes provider-local facets for manifest, discovery, and
// execution at the composition edge. Provider availability never depends on
// package initialization or mutable global registration.
//
// Provider-hosted web search follows protocol grammar by default. Responses,
// Chat Completions, and Messages each own one typed lowering which every target
// using that protocol inherits. Provider identity never suppresses the
// declaration: backend acceptance or rejection is authoritative.
//
// An exact provider codec exists only for demonstrated non-default wire
// grammar and composes that spelling before the protocol's single JSON
// serialization. This is typed encode-time composition, not serialized JSON
// mutation, provider capability prediction, hostname detection, or a dialect
// registry.
package providers
