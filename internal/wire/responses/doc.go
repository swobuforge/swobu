// Package responses maps canonical response-generation semantics to and from
// the responses wire protocol.
//
// It owns continuity-preserving request encoding, including ordered
// declaration contributions, preserved namespace trees, and semantic
// tool-search call/output lifecycles. Canonical structured-output lowers through
// text.format, and canonical tool-call batch lowering through
// parallel_tool_calls. Plain flat tool names stay raw; namespace-bearing tool
// names project only when the flat wire needs to carry scope. The package also
// owns semantic success-stream decoding for
// responses-specific behavior such as function-call argument streaming and
// reasoning-token usage accounting.
// User-message and tool-result images preserve URL or inline sources; tool
// results retain ordered mixed content through Responses content arrays.
// Request diagnostics correlate full and implicitly rebased decode views by
// carrier exchange metadata and report only safe image counts, coordinates,
// call/tool identity, and source kind.
// Historical custom-tool calls carry a self-contained canonical key in the
// established request-origin namespace and do not require a declaration in the
// current effective tool environment. Their input member is required but may
// contain empty text; all text, including whitespace, remains exact.
// Historical custom-tool outputs reuse the canonical ToolResultItem. Replay
// derives their wire discriminator from the correlated call in full canonical
// history, including when only a continuation segment is being encoded.
// Responses summary and reasoning_text output map through portable canonical
// reasoning. Every complete response.output item is decoded into the one
// canonical history graph: known semantics use typed branches and refinements,
// while unknown item types alone remain opaque. A usable previous_response_id
// selects target-bound Delta continuation; otherwise the codec lowers canonical
// Full history without a parallel native transcript. Synthetic client response
// item IDs are presentation-only.
// Buffered and streaming
// paths complete the same canonical items. Stream projection keeps web-search
// calls in the web_search_call grammar until their typed action and optional
// sources are complete; it never aliases them to function_call argument
// events. Progressive output remains correlated in provider coordinates while
// one-to-many lifecycle projection assigns independent canonical ordinals.
// Client request decoding uses the shared ingress helper for top-level
// request parsing and normalizes stringified
// function_call.arguments payloads emitted by OpenAI-family client bridges.
// Ordered input and appendable output items define the private responses
// history fingerprint scheme. Top-level invocation fields remain on rebased
// requests but do not identify history; response envelopes do not participate.
// Stateless client history may omit output-item presentation IDs or retain an
// actionless completed web-search marker. Observed actions receive only a
// request-local pairing ID; the actionless case is a typed partial-lifecycle
// marker, and canonical does not fabricate an unobserved action. Stateless
// request lowering folds typed WebSearchCall/WebSearchResult pairs back into
// one web_search_call by canonical call ID, including non-adjacent results.
// Provider request encoding always lowers canonical Full or Delta. Only
// Responses facts with named behavioral consumers enter canonical: encrypted
// reasoning, invocation reasoning context, and ordered additional_tools
// declarations. Ordinary additional_tools is history-scoped. Only a complete
// prefix carrying the HTTP Lite marker or WebSocket metadata equivalent becomes
// request-scoped; marker evidence never enters canonical. Unsupported contained
// tool discriminators report their safe wire
// location and observed type without exposing declaration payloads. Request
// item kinds without an implemented semantic projection are rejected; unknown
// provider output and actionless search markers are not durable conversation
// state.
// The nullable boolean client `store` field is validated and discarded at this
// wire boundary. It is not canonical state and cannot govern the mandatory
// session checkpoint committed by exchange execution.
package responses
