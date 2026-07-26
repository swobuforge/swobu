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
//   - Explicit-ID versus exact client-history predecessor selection, atomic
//     rebased-request handling, history composition, canonical response capture,
//     and post-client-encode commit
//   - Lazy, attempt-scoped Responses ancestry loading only when a selected
//     Responses attempt cannot use exact native delta continuation
//   - Delivery conversion contract and exact-backend orchestration
//   - The client codec bridge surface; provider codecs live behind provider.Backend
//   - Opening one fully initialized request-scoped MCP runtime before candidate
//     selection
//   - Delayed handoff, provider re-entry, usage accumulation, and fallback
//     closure as consequences of runtime-owned MCP calls
//   - Monotonic closure of provider fallback before the first MCP side effect
//
// It does NOT own:
//   - Routing policy (internal/routing)
//   - Provider adapters (internal/adapters/outbound/providers)
//   - Canonical MCP source/tool meaning (internal/domain/canonical)
//   - MCP access, sessions, catalogs, ownership, budgets, protocol mechanics,
//     and network hardening (internal/mcp)
//   - A generic tool runtime
//
// Import rules:
//   - exchange → routing, provider, session, profile, observation, domain, mcp
//   - Nothing may import exchange except adapters and bootstrap.
//
// Remote MCP enters as concrete runtime-open, batch-reservation, and call
// commands/events.
// Exchange stores one runtime pointer and no SDK sessions, bearer maps, catalog
// indexes, executor interfaces, registries, pools, or provider-native MCP path.
// New provider representations add explicit transient request choices and
// closed transitions only when an implemented alternative exists. Attempt
// requirements record issued-call facts; generic provider errors never infer
// which feature caused a failure. Alternatives do not add nested retry loops,
// synchronized route cursors, or phase booleans. Request-image materialization
// is protocol-independent and malformed or unavailable client media fails the
// exchange. A later typed backend-local codec rejection may advance routing.
package exchange
