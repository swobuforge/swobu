// Package requestpath owns the application-layer orchestration for one client
// protocol request lifecycle.
//
// It coordinates endpoint intent, canonical continuation semantics,
// provider execution, model-resolution defaults for endpoint routing, normalized
// ingress provenance carried from adapters, and runtime evidence emission.
//
// Requestpath also owns semantic middleware orchestration used to compose
// canonical middleware policies (timeout, continuation recovery, tool-choice
// immediate downgrade retry, evidence) around one terminal provider execution call.
// Execution-time capability snapshots and execution contracts are carried in
// requestpath orchestration context and provider-port requests, separate from
// canonical request meaning. Capability truth is resolved from a startup-built
// backend-model capability catalog keyed by provider/protocol/model identity,
// sourced from domain provider/model capability facts.
// It also owns endpoint-qualified model listing truth for
// configured routes without redefining the domain meaning of requests,
// outputs, or errors.
package requestpath
