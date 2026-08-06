// Package messages maps canonical requests and responses to the Messages wire
// grammar. It owns family-level request/response encoding, stream reduction,
// deterministic flat tool names, exact opaque thinking replay, and the private
// client-history fingerprint. Opaque replay validates the Messages block kind
// while retaining provider-owned additive fields. Provider authentication,
// transport, dialects, and target-specific capability rejection stay outside.
//
// Unknown additive occurrences erase locally and preserve sibling order.
// Closed request contracts, ambiguous flat names, invalid residual containers,
// and contradictory stream lifecycle or identity fail at their owning
// boundary. Buffered and streamed delivery must converge on the same surviving
// canonical semantics.
//
// Static top-level system/tools fields cannot represent arbitrary mid-history
// activation order. Lowering may hoist only the proven leading prefix; other
// cases are target incompatibilities. Provider adapters must reject unsupported
// native fields, such as Mantle structured output, before network I/O.
package messages
