// Package exchange is the request-path bounded context: ingress, orchestration,
// and execution contract.
//
// This package owns:
//   - Client request ingress (wire decode, endpoint resolution)
//   - Routing orchestration (one reducer-owned state lifecycle per request)
//   - Ordered provider-call attempts and all selection of further provider work
//   - One exchange-scoped URL fetch cache reused by candidate attempts without
//     becoming checkpoint truth
//   - Winning-attempt media bindings, merged only with inherited durable
//     bindings and retained by exact request-part occurrence
//   - Explicit workspace-slug partitioning for the daemon-global checkpoint store
//   - Delivery conversion contract and exact-backend orchestration
//   - The client codec bridge surface; provider codecs live behind provider.Backend
//   - The reducer-owned provider-result edge where future concrete tool
//     classification may be admitted before client handoff
//
// It does NOT own:
//   - Routing policy (internal/routing)
//   - Provider adapters (internal/adapters/outbound/providers)
//   - Canonical MCP semantics, MCP networking, attempt-local MCP lowering, or
//     a generic tool runtime
//
// Import rules:
//   - exchange → routing, provider, session, profile, observation, domain
//   - Nothing may import exchange except adapters and bootstrap.
//
// Future external tool work must enter as a concrete command, event, and phase
// only when a feature supplies real I/O and lifecycle ownership. This package
// does not keep dormant tool phases or executor interfaces for extensibility.
// New provider representations add explicit transient request choices and
// closed transitions only when an implemented alternative exists. Attempt
// requirements record issued-call facts; generic provider errors never infer
// which feature caused a failure. Alternatives do not add nested retry loops,
// synchronized route cursors, or phase booleans. Candidate-scoped preparation
// failure advances routing; malformed request-global media fails the exchange.
package exchange
