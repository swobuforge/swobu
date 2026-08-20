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
// Runtime target availability is ephemeral Exchange state. It cannot enter the
// routing aggregate or alter deterministic plan construction.
// Equal-tier target ordering is a deterministic projection of an opaque cache
// placement key, route name, tier index, and stable target IDs. Routing treats
// that key as opaque; its caller owns cache-locality semantics. Configured position
// inside a tier is normalized away because Tier assigns it no semantics.
//
// Route resolution gives exact configured names precedence. Any other
// non-empty client model token selects the explicitly configured workspace
// default route. This supports clients that cannot emit Swobu route names while
// keeping provider inference and runtime alias creation out of routing. Missing
// or blank tokens, and unmatched tokens in a workspace without a default, fail.
//
// Each target has one durable ID and one daemon-owned durable monotonic version. Its
// protocol records the provider whose catalog admitted it and must match the
// typed connection provider. Effective setting equality compares durable
// fields explicitly. Target settings and credential-reference saves advance
// the version; session resolution uses it to reject native handles captured
// before a target save. A workspace retains the highest committed generation
// even while an ID is absent, so delete/re-add cannot revive stale native state.
// Operator route specifications never carry the version; materialized
// persistence restores active versions and generation history across restarts.
//
// A standard connection persists the provider, effective locator, and optional
// credential. Provider-specific locator shorthand is normalized before this
// value is constructed; it does not create a new durable connection arm.
//
// A Bedrock connection persists the signing region and an optional explicit API
// URL as separate facts. An empty endpoint remains empty in routing; effective
// endpoint, request, and catalog URLs are derived only after protocol selection.
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
