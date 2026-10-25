// Package accounting owns typed Workspace spend limits and reservations.
//
// Limit publication is transactional: persistence failure retains the prior
// in-memory decision and revision. Approved Limits management stores a closed
// credential-free change receipt in the same atomic replacement as the limit,
// with approval identity for idempotency and session correlation for audit.
// These receipts are local Operator data, never model diagnostics. This package
// rejects incomplete, duplicate or invalid before/after audit receipts on reopen
// without repairing or rewriting authoritative bytes. Their absence is not a
// claim that unaudited direct configuration or other owners never changed state.
// does not authenticate operators, approve proposals, or grant session scope.
// Limit.Validate owns the closed limit contract for preview, publication and
// durable reopen. Unknown pricing is rejected in enforcing mode; observe-only
// is selected through Mode, never an alternate unknown-price policy string.
// Invalid persisted limits fail reopen without rewriting the accounting file.
// Reopen also rejects malformed clock/window keys, incomplete or duplicate
// reservation identities and unknown state/coverage values. Historical omitted
// coverage remains explicitly unknown; corrupt authority is never repaired by
// dropping charges or resetting the clock floor.
// Budget thresholds are positive; access suspension belongs to permission
// revocation, not a zero-dollar budget. Zero-cost operations and settlements
// remain valid amounts and do not imply a zero threshold.
// Reservation and settlement amounts cannot be negative. Enforcing admission
// subtracts each charge from remaining capacity, avoiding signed overflow even
// when observed charges exceed the representable aggregate. Observe-only keeps
// those individual charges intact; it does not claim an enforced financial cap.
// Quoted reservations synchronously retain target generation, exact economic
// version and original quantity forecast. Settlement changes the charged amount,
// never that original quote. Projections are detached; replay preserves the
// admitted quote rather than repricing an existing attempt. Historical unquoted
// reservations stay unquoted, without fabricated price or quantity provenance.
// Terminal provider usage can settle the frozen vector only when totals and
// cache/reasoning subset quantities are known and internally consistent. Missing
// billable rates or quantities retain the conservative charge as uncertain.
// Status projects current UTC-window synchronous charges, reservations and
// uncertainty without moving the persisted clock floor. Exact decimal-string
// totals cannot overflow or lose browser precision; partial/unquoted operations
// remain visible and are never labelled fully covered provider invoices.
package accounting
