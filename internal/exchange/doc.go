// Package exchange owns stage order and orchestration contracts for one
// request/response exchange.
//
// It coordinates stages, route and delivery selection, transform chains,
// effect commit, and error boundaries. Tool policy and declaration semantics
// are consumed here but owned by canonical request state; exchange may
// materialize or clear native continuation, but it does not redefine the tool
// ontology or embed provider-specific tool behavior. The canonical architecture
// overview lives in docs/03-architecture/system-shape-and-request-flow/
// exchange-algebra-and-boundaries.md, and the required exchange conformance
// cases live in docs/05-engineering/testing-and-quality-gates/
// domain-contract-integration-and-end-to-end-tests.md.
package exchange
