// Package responses maps canonical requests and responses to the Responses wire
// grammar. It owns request admission, full/native-delta lowering, response
// projection, stream reduction, and the client-history fingerprint. Canonical
// remains the only portable graph; this package retains no parallel transcript.
//
// Unknown additive occurrences erase locally and preserve sibling order.
// Missing discriminators, closed-contract violations, invalid residual
// containers, and contradictory lifecycle or identity fail at their owning
// boundary. Buffered and streamed delivery must converge on the same completed
// canonical semantics. Terminal-checkpoint mismatch diagnostics expose only
// canonical item shape and a closed mismatch category; provider frames and
// inference content never enter logs. An indexed output's first non-empty
// identity remains authoritative when later terminal snapshots omit it, while
// repeated non-empty identity must agree. Reasoning checkpoint equivalence is
// defined by ordered readable parts and opaque Responses replay semantics, not
// private nil-versus-empty storage shape.
//
// An unfamiliar web-search status erases only that lifecycle refinement. The
// known call survives without a synthesized result, and provider completion
// remains subject to canonical settlement.
//
// Known MCP declarations retain typed URL, connector, tunnel, selection,
// approval, loading, and caller semantics. Authorization and headers remain
// transient. Approval-free native declarations lower exactly; approval-bearing
// attempts reject as target-incompatible until the canonical approval
// request/response lifecycle exists. Malformed MCP syntax and unsolicited
// provider-owned MCP effects still fail.
//
// Shared lowering implements official Responses with compatibility-oriented
// representation choices: full materialized history, flat scope-qualified
// ordinary tools, eagerly materialized known declarations, and metadata-light
// replay. Exact provider codecs may add only positively owned wire details.
// Foreign opaque reasoning remains in canonical checkpoint truth but enters
// Responses output and history fingerprints only when a Responses replay or
// client-visible summary exists; projected history must describe only what the
// client can append on its next full-history request.
// Provider-resolved web-search details remain in canonical truth and wire
// output, while history fingerprints use the completed lifecycle marker and
// text-only assistant continuation that Codex appends on replay.
// Text-only results split across several canonical parts concatenate into the
// official string form with approximation evidence; image-bearing results retain
// their rich array.
// Client response projection preserves a resolved function call's namespace in
// buffered and streaming output. Flat provider aliases are attempt-local only;
// their allocated wire identities, rather than canonical leaf names, define
// flat declaration uniqueness and must reverse to the exact client-executable
// namespace and leaf identity.
package responses
