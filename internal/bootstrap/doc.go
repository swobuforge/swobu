// Package bootstrap wires the local Swobu runtime.
// Models publication recovery runs under the routing lifetime lock before
// routing decode/create, registry open or listener composition. Candidate
// documents pass the normal complete store decoders before recovery publishes.
// After routing opens, committed retained replay is independently reconstructed
// and installed while the lifetime lock remains held, before serving commands.
// Verification/installation failure closes the store and releases that lock.
// Credential source retirement follows routing/Connection and selected MCP/Search
// registry reopen, before provider writers/listeners. Exact committed reservations
// and every destination are verified before deletion; retained unused sources
// also prevent deletion. Cleanup failure emits a sanitized maintenance event and
// keeps committed configuration authoritative for retry on a later startup.
// Startup and periodic owned-slot cleanup preserve pending migration copies even
// after their Connection rotates or is deleted. Verified source retirement then
// records completion, releasing obsolete copies to the existing cleanup lifecycle.
// Listener binding precedes address-bearing Operator mux composition. A port-zero
// request must publish the actual bound address to Connect and login projections;
// composition failure closes the bound listener before startup returns.
//
// It owns startup composition for the daemon process boundary: config loading,
// dependency wiring, listener startup, and the minimal runtime status truth the
// rest of the process can query.
//
// Bootstrap also composes embedded telemetry runtime export as a downstream
// consumer of traffic evidence append events (plus local install state) rather
// than a request-path control input. It does not own HTTP rendering, domain
// rules, or provider dialect behavior.
// Canonical Node status mounts the existing redacted runtime health projection
// at /api/v1/status behind local Control admission. It introduces no parallel
// health authority; legacy consumer retirement remains explicit cutover work.
// Local admission wraps the router directly, so path rejection precedes both
// managed credential resolution and ServeMux's path-cleaning redirects.
// Canonical Node Control projects Access-owned identities as Principals;
// the obsolete /_swobu namespace is not mounted. Host-local shutdown and
// telemetry inspection use canonical /api/v1/daemon/shutdown and
// /api/v1/telemetry/report, without a credential-directory browsing API.
// MCP binding refresh closes stale sessions before reopening a changed
// Connection. Failed discovery leaves the binding unavailable; later requests
// can reopen the current revision without reviving a prior credential.
// Direct Workspace MCP calls recheck live authority at source dispatch and
// record standalone content-free operations in the shared Activity evidence
// store. Transport errors retain may-have-occurred truth; no LLM Exchange is
// fabricated, and arguments, results and credential material are excluded.
// Ledger opens beside the active configuration by default, or uses explicit
// off/PostgreSQL settings. Invalid preferences reject startup; unavailable
// storage degrades runtime without changing request execution. Shutdown drains
// HTTP handlers before the bounded writer and closes the independent ChatGPT
// loopback auth listener, canceling any pending browser handoffs. Standalone
// auth-start intent markers remain on disk across abrupt process death; a
// repeated canonical start is rejected as indeterminate instead of being
// dispatched again. These markers preserve dispatch attribution, not provider
// session recovery. Standalone settlement feeds Ledger;
// Activity merges bounded durable standalone history with live memory by exact
// operation identity, preserving live records and ordering before consumer limits.
// Durable read/open failure is explicit, not an empty-history fallback. Bounded
// Exchange history reuses the evidence owner's lifecycle projection, preserving
// model attempt/usage semantics; both completed kinds feed the shared writer.
// Node-wide Activity carries content-free process-lifetime Ledger counters;
// Workspace views omit deployment-wide health to avoid cross-scope leakage.
// Retained records do not imply complete capture, nor do fresh restart counters
// establish absence of earlier loss.
// Retained Activity lists and local-only exports share activityPage, including
// indexed Exchange predicates and opaque Workspace/kind-bound continuation.
// Disabled Ledger fails these retained reads; public ingress never mounts the
// export endpoint. Live snapshots and SSE do not accept retained-only filters.
// Optional SWOBU_PRICING_CONFIG supplies an absolute accepted Models database
// and explicit target-generation/provider/selector/endpoint/tariff bindings.
// Positive operator token forecasts produce partial quotes, not token-count or
// maximum-bill claims. Unmatched or unavailable prices remain unknown; invalid
// settings reject startup. Reader lifetime ends after handler shutdown.
// Workspace MCP and Search source attachment/removal are mounted only beneath
// /api/v1/workspaces; remaining legacy Workspace operations retain their own
// migration qualification before the broader mount retires.
// The relay carries only published Workspace data paths. The single Node-owned
// publication set controls reachability, while the same Workspace credentials,
// Service grants and limits control operations at both local and remote ingress.
// Local Operator Control and the browser are never mounted in the relay handler.
// Publishing acquires a temporary relay-readiness lease through durable Node
// publication. Removing the final publication stops an otherwise idle relay.
// Public MCP uses the same scoped Workspace admission and tool catalog as local
// MCP, without a second protocol-mask or credential authority.
// Direct Search resolves the current Connection revision and credential for
// each candidate attempt; no startup Search binding cache survives edits.
// Captured in-flight sources keep their endpoint and credential across edits.
// Direct Search evidence reopens its bounded runtime head and flushes after HTTP
// shutdown. Retention failure degrades status without changing Search execution.
// Public direct Search uses the same Workspace Search handler as local ingress,
// including live Access service/rate admission and the Workspace accounting owner.
// Unknown Search cost remains unknown and obeys the configured price policy.
package bootstrap
