// Package routing owns the immutable workspace/route/tier/target aggregate,
// whole-value invariants, semantic edits, exact route resolution, and
// deterministic monotonic attempt-plan construction. Daemon startup
// preferences, including daemon address, remain outside this aggregate.
//
// Routing is the product-domain boundary below operator and request-path
// adapters. It contains no persistence, transport, UI, exchange, or provider
// catalog mechanism. Construction edges supply catalog predicates to the one
// TargetDraft finalizer; runtime consumers receive immutable values and never
// reconstruct routes from DTOs.
//
// The canonical model lives in
// docs/03-architecture/system-shape-and-request-flow/workspace-routing-configuration-and-local-persistence.md
// and docs/03-architecture/system-shape-and-request-flow/monotonic-routing-boundary-and-attempt-semantics.md.
//
// Boundary rules:
//   - No dependency on internal/exchange.
//   - No dependency on provider catalog (internal/profile).
//   - No dependency on internal/profile, configstore, transport, or Cockpit.
//   - No mutable collection storage crosses the package boundary.
package routing
