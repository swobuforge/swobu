// Package responses maps canonical exchanges to the OpenAI Responses API.
//
// Canonical values own conversation state; this codec does not keep a separate
// transcript. Explicit Codex Lite ingress markers identify a request-owned
// prefix of optional tool declarations and optional base instructions, not a
// mandatory tool list. Contradictory top-level fields fail before lowering.
// Target lowering resolves the canonical schema contract to
// Responses strict controls and compatibility evidence before DTO serialization.
// Web-search domain restrictions fail source admission because canonical search
// cannot represent them. This rejection precedes target selection and cannot
// establish a target capability gap.
// Tool aliases remain attempt-local and resolve back to the client's tool
// identity. MCP authorization headers are transient inputs for local execution
// and must not enter provider request history.
package responses
