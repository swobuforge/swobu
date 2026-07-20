// Package responses maps canonical response-generation semantics to and from
// the responses wire protocol.
//
// It owns continuity-preserving request encoding, including Responses namespace
// flattening at the wire edge, canonical structured-output lowering through
// text.format, and canonical tool-call batch lowering through
// parallel_tool_calls. Plain flat tool names stay raw; namespace-bearing tool
// names project only when the flat wire needs to carry scope. The package also
// owns semantic success-stream decoding for
// responses-specific behavior such as function-call argument streaming and
// reasoning-token usage accounting.
// User-message and tool-result images preserve URL or inline sources; tool
// results retain ordered mixed content through Responses content arrays.
// Responses summary output maps through portable canonical reasoning. P0 does
// not admit reasoning context, reasoning_text, encrypted_content, or manual
// reasoning-item replay; native previous_response_id remains the sole exact
// continuation mechanism. Synthetic response item IDs are presentation-only.
// Buffered and streaming
// paths complete the same canonical items. Client request decoding uses the shared ingress
// helper for top-level request parsing and normalizes stringified
// function_call.arguments payloads emitted by OpenAI-family client bridges.
// Ordered input and appendable output items define the private responses
// history fingerprint scheme. Top-level invocation fields remain on rebased
// requests but do not identify history; response envelopes do not participate.
package responses
