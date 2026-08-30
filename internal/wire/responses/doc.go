// Package responses maps canonical requests and responses to the Responses wire
// grammar. It owns request admission, full/native-delta lowering, response
// projection, stream reduction, and the client-history fingerprint. Canonical
// remains the only portable graph; this package retains no parallel transcript.
//
// A current provider-hosted WebSearch declaration is target-selecting when the
// effective tool policy permits it to execute: only an exact target dialect
// may lower it, and an unhandled executable declaration makes that candidate
// incompatible before dispatch. Policy-dead declarations omit locally.
// Settled historical WebSearch calls
// and results are transcript facts, not current capability requirements; the
// conversation projector replays them independently without re-execution.
// Request projection asks for web_search_call.action.sources only when the
// selected WebSearch projection explicitly owns that target capability;
// fragment emission and client/provider identity do not imply support.
// Unknown additive declarations erase locally and preserve sibling order.
// Missing discriminators, closed-contract violations, invalid residual
// containers, and contradictory lifecycle or identity fail at their owning
// boundary. Buffered and streamed delivery converge on portable completed
// semantics and each preserve usable provider replay; opaque replay bytes need
// not match across those distinct authority paths. Each provider output index
// has one slot with an explicit accumulating, awaiting-terminal, or done phase.
// The first successfully
// admitted response.output_item.done owns that occurrence's immutable canonical
// semantics; later item-done or response-terminal copies may confirm identity
// but cannot rewrite or invalidate content. Incomplete item-done content remains
// authoritative while publication waits for a compatible response outcome.
// Response terminals own response-level outcome and backfill only indexes that
// never produced an admitted item-done. Compatibility evidence comes only from
// the observation used to construct canonical semantics. The slot owns the
// first non-empty output identity, reconstructs later omissions, and rejects
// repeated non-empty identity mutation. Encrypted replay remains opaque item
// content and is never compared across duplicate observations.
//
// An unfamiliar web-search status erases only that lifecycle refinement. The
// known call survives without a synthesized result, and provider completion
// remains subject to canonical response validation.
//
// Known MCP declarations retain typed URL, connector, tunnel, selection,
// approval, loading, and caller semantics. Authorization and headers remain
// transient and flow only to Exchange's local MCP runtime. Provider lowering
// rejects every residual MCP declaration; unsupported local forms fail before
// dispatch. Malformed MCP syntax and unsolicited provider-owned MCP effects
// still fail.
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
