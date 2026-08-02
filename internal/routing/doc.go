// Package routing owns the immutable workspace/route/tier/target aggregate,
// whole-value invariants, semantic edits, default-compatible route resolution, and
// deterministic ordered target-plan construction. Daemon startup
// preferences, including daemon address, remain outside this aggregate.
//
// Routing is the product-domain boundary below operator and request-path
// adapters. It contains no persistence, transport, UI, exchange, or provider
// catalog mechanism. Construction edges supply catalog predicates to the one
// TargetDraft finalizer; runtime consumers receive immutable values and never
// reconstruct routes from DTOs.
//
// A target's position in a built plan is possible work, not provider I/O.
// Exchange owns the issued provider-call attempt lifecycle, and one position
// may produce zero or more calls without changing routing order.
//
// Route resolution gives exact configured names precedence. Any other
// non-empty client model token selects the explicitly configured workspace
// default route. This supports clients that cannot emit Swobu route names while
// keeping provider inference and runtime alias creation out of routing. Missing
// or blank tokens, and unmatched tokens in a workspace without a default, fail.
//
// Each target has one durable ID and one process-local monotonic version. Its
// protocol records the provider whose catalog admitted it and must match the
// typed connection provider. Effective setting equality compares durable
// fields explicitly. Target settings and credential-reference saves advance
// the version; session resolution uses it to reject native handles captured
// before a target save. Version is intentionally process-local.
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
