// Package exchange owns orchestration contracts for one request/response
// exchange.
//
// It coordinates route and delivery selection, typed ports, same-port links,
// graph registry path search, predicates, middleware, runtime codec lookup,
// ordered fallback wrapping, replay buffering, commit gating, effect commit,
// and error boundaries. Tool
// policy and declaration semantics are consumed here but owned by canonical
// request state; exchange may materialize or clear native continuation, but it
// does not redefine the tool ontology or embed provider-specific tool
// behavior.
// The canonical architecture overview lives in
// `docs/03-architecture/system-shape-and-request-flow/exchange-algebra-and-boundaries.md`,
// and the required exchange conformance cases live in
// `docs/05-engineering/testing-and-quality-gates/domain-contract-integration-and-end-to-end-tests.md`.
package exchange
