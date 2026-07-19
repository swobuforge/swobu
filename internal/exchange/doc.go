// Package exchange is the request-path bounded context: ingress, orchestration,
// and execution contract.
//
// This package owns:
//   - Client request ingress (wire decode, endpoint resolution)
//   - Routing orchestration (one reducer-owned state lifecycle per request)
//   - Explicit workspace-slug partitioning for the daemon-global replay store
//   - Delivery conversion contract and exact-backend orchestration
//   - The client codec bridge surface; provider codecs live behind provider.Backend
//   - The reducer-owned provider-return edge where future concrete tool
//     classification may be admitted before client handoff
//
// It does NOT own:
//   - Routing policy (internal/routing)
//   - Provider adapters (internal/adapters/outbound/providers)
//   - Canonical MCP semantics, MCP networking, attempt-local MCP lowering, or
//     a generic tool runtime
//
// Import rules:
//   - exchange → routing, provider, replay, profile, observation, domain
//   - Nothing may import exchange except adapters and bootstrap.
//
// Future external tool work must enter as a concrete command, event, and phase
// only when a feature supplies real I/O and lifecycle ownership. This package
// does not keep dormant tool phases or executor interfaces for extensibility.
package exchange
