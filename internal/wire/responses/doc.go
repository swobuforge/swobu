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
// Responses reasoning output is not part of the current canonical v0 band and
// must fail closed with a response.reasoning rejection decision instead of
// disappearing from decode. Client request decoding uses the shared ingress
// helper for top-level request parsing and normalizes stringified
// function_call.arguments payloads emitted by OpenAI-family client bridges.
package responses
