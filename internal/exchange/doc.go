// Package exchange owns orchestration contracts for one request/response
// exchange.
//
// It composes typed ports, links, predicates, stage mechanics, runtime codec lookup,
// graph-path selection, replay buffering, commit gating, effect commit, and
// error boundaries. Ordered fallback advances across candidate-local
// non-internal failures, but internal failures stay terminal. Tool policy and
// declaration semantics are consumed here but owned by canonical request
// state; exchange may materialize safe native continuation or fail closed on
// unsafe replay selection, but it does not redefine the tool ontology or embed
// provider-specific tool behavior.
// It also owns the typed boundary result carrier and client request decode
// bundle used by wire codecs and exchange orchestration.
// The canonical architecture overview lives in
// `docs/03-architecture/system-shape-and-request-flow/exchange-algebra-and-boundaries.md`,
// and the required exchange conformance cases live in
// `docs/05-engineering/testing-and-quality-gates/domain-contract-integration-and-end-to-end-tests.md`.
package exchange
