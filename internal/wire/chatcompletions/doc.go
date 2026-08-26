// Package chatcompletions maps canonical requests and responses to the Chat
// Completions wire grammar. It owns request/response encoding, stream reduction,
// deterministic flat tool names, and the client-history fingerprint for this
// family. Provider dialects, endpoint selection, and routing stay outside.
//
// Standard lowering omits a settled provider-hosted WebSearch call/result pair
// from request history when Chat has no replay grammar, while preserving the
// portable assistant continuation and recording request-item loss at the
// canonical call position. Preserved cited text also records request-part
// citation loss; Chat never synthesizes citation text or source summaries.
// Unresolved history and active WebSearch declarations remain incompatible;
// only an exact target dialect may replace those semantics. Unknown additive list
// occurrences erase locally and preserve sibling order.
// Closed request contracts, ambiguous projected tool names, invalid residual
// responses, and contradictory stream identity fail at their owning boundary.
// Buffered and streamed delivery must converge on the same surviving canonical
// semantics.
//
// Chat has no native citation or portable reasoning-output grammar, so those
// losses are explicit. Tool-result images use a backend-only synthetic user
// message only after the active call batch is closed; the projection never
// becomes trusted client history and therefore has no inverse carrier.
package chatcompletions
