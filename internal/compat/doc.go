// Package compat owns descriptive decisions emitted by concrete representation
// seams. Swobu has no compatibility behavior mode: each seam has one safe
// best-effort lowering. Safe best effort tries Exact, then an explicitly
// bounded Approx, then occurrence-local Drop only when surviving semantics are
// unchanged and valid; otherwise the owning operation fails. Decisions are
// post-operation evidence only. They never approve a degradation, define
// behavior modes, become provider declarations or routing inputs, enter
// session state, or predict future support.
package compat
