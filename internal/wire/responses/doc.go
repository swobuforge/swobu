// Package responses maps canonical requests and responses to the Responses wire
// grammar. It owns request admission, full/native-delta lowering, response
// projection, stream reduction, and the client-history fingerprint. Canonical
// remains the only portable graph; this package retains no parallel transcript.
//
// Unknown additive occurrences erase locally and preserve sibling order.
// Missing discriminators, closed-contract violations, invalid residual
// containers, and contradictory lifecycle or identity fail at their owning
// boundary. Buffered and streamed delivery must converge on the same completed
// canonical semantics.
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
package responses
