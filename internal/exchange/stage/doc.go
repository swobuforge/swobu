// Package stage owns exchange-scoped stage mechanics for same-port links and
// stage-selected wrappers.
//
// It is an orchestration substrate, not the boundary grammar. Concrete
// codecs, semantic lowerers, wire patchers, validators, projectors, and
// decision emitters live in their own boundary packages and are classified in
// the design docs. Stage-local execution returns a result shape that carries
// the next value, mutation truth, and emitted effects so patch and wrapper
// semantics stay in one owner noun.
package stage
