// Package chatcompletions maps canonical conversation semantics to and from the
// chat completions wire protocol.
//
// It owns request encoding, including canonical structured-output lowering
// through response_format, deterministic projected tool names for flat
// function/custom tool surfaces, and canonical tool-call batch lowering
// through parallel_tool_calls. Chat completions lower an explicit canonical
// max output limit using the exact backend policy supplied by composition and
// accepts both max_completion_tokens and max_tokens on decode. Leading inbound
// system/developer messages become request-scoped directives; later occurrences
// remain history-scoped. Declaration contributions are projected through the
// effective tool environment into the static tools field; historical activation
// order is recorded as an approximation when it must be hoisted. The package also owns
// success-stream decoding for this
// protocol only, including reasoning-token usage accounting when the wire shape
// provides it. Standard reasoning_effort belongs to this protocol grammar.
// Provider-hosted web search follows the OpenAI-owned Chat Completions grammar:
// a top-level web_search_options object rather than an entry in tools. Exact
// providers with a different spelling replace that lowering before the single
// serialization boundary. Other provider dialects remain outside this package.
// Empty tool surfaces omit tool_choice because the choice is inert
// there and some backends reject an explicit no-tool field. It must not take on
// endpoint selection, provider wiring, or non-chat public contract semantics.
// Client request decoding also normalizes stringified function_call.arguments
// payloads emitted by OpenCode-style client bridges.
// User images preserve URL or inline sources. Tool-result images project into
// one attempt-local synthetic user image message only after the active assistant
// tool-call batch is provably closed; deterministic markers retain canonical
// call/item/part association, and the projection never becomes history.
// Unsupported explicit detail is safely approximated with compatibility evidence.
// Client-visible messages define the private chat-completions history
// fingerprint scheme. Root invocation fields remain on rebased requests but do
// not identify history; response envelopes do not participate.
package chatcompletions
