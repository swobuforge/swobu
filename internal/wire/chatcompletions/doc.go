// Package chatcompletions maps canonical conversation semantics to and from the
// chat completions wire protocol.
//
// It owns request encoding, including canonical structured-output lowering
// through response_format, deterministic projected tool names for flat
// function/custom tool surfaces, and canonical tool-call batch lowering
// through parallel_tool_calls. Chat completions lower an explicit canonical
// max output limit using the exact backend policy supplied by composition and
// accepts both max_completion_tokens and legacy max_tokens on decode. Canonical instructions
// lower to a leading system message; inbound system/developer messages decode
// back into that canonical instruction band instead of user-authored
// conversation items. The package also owns success-stream decoding for this
// protocol only, including reasoning-token usage accounting when the wire shape
// provides it. Empty tool surfaces omit tool_choice because the choice is inert
// there and some backends reject an explicit no-tool field. It must not take on
// endpoint selection, provider wiring, or non-chat public contract semantics.
// Client request decoding also normalizes stringified function_call.arguments
// payloads emitted by OpenCode-style client bridges.
package chatcompletions
