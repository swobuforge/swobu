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
// Responses summary and reasoning_text output map through portable canonical
// reasoning. Independently, every complete native response.output item is
// captured in order for exact stateless continuation; encrypted, program, and
// unknown state remains opaque. A usable previous_response_id selects native
// delta continuation. Otherwise a selected Responses attempt may replay a
// complete native checkpoint ancestry and falls back to portable full history
// when that optional ancestry is unavailable. Synthetic client response item
// IDs are presentation-only.
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
// request-local pairing ID; actionless markers remain exact native input and
// do not acquire fabricated portable meaning.
// Provider request encoding selects exact native input and stateless native
// history before attempting portable conversation lowering. Exact providers
// compose typed tool and input fields before this package's one serialization.
// The independent replay transcript is owned by domain/responsesnative rather
// than canonical because it is carried beside, not beneath, semantic request
// and response artifacts.
// The nullable boolean client `store` field is validated and discarded at this
// wire boundary. It is not canonical state and cannot govern the mandatory
// session checkpoint committed by exchange execution.
package responses
