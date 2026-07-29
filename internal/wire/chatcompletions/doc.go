// Package chatcompletions maps canonical requests and responses to the Chat
// Completions wire grammar. It owns request/response encoding, stream reduction,
// deterministic flat tool names, and the client-history fingerprint for this
// family. Provider dialects, endpoint selection, and routing stay outside.
//
// Unknown additive list occurrences erase locally and preserve sibling order.
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
