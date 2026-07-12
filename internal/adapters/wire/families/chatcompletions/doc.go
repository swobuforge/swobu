// Package chatcompletions maps canonical conversation semantics to and from the
// chat completions wire protocol.
//
// It owns request encoding, including canonical structured-output lowering
// through response_format, deterministic projected tool names for flat
// function/custom tool surfaces, and canonical tool-call batch lowering
// through parallel_tool_calls, plus success-stream decoding for that protocol
// only, including reasoning-token usage accounting when the wire shape
// provides it. It must not take on endpoint selection, provider wiring, or
// non-chat public contract semantics. Client request decoding also normalizes
// stringified function_call.arguments payloads emitted by OpenCode-style
// client bridges.
package chatcompletions
