// Package credentials owns credential resolution adapters.
//
// Resolver implementations translate explicitly scoped, operator-selected
// credential references into values at the consuming execution edge. The
// credentialref.Scope value separates provider identity from reusable Connection
// identity. MCP HTTP/stdio and Search resolve through the exact Connection
// scope; no source-owned materialization or resolution path remains.
//
// Connection-owned material uses a distinct scope and random immutable slots.
// Resolution and retirement require the exact Connection ID; provider
// consumers cannot use this namespace. Retirement accepts only generated stored
// slots and must follow durable publication plus live-reference exclusion.
// Registered-slot writes compare material inside this adapter and reuse an
// existing locator only for identical execution material; conflicting retries
// never overwrite it. Unavailable lookup fails closed rather than treating an
// uncertain prior write as absent. No material digest is stored in config.
// The shared credential file serializes all adapter instances and publishes
// replacement documents atomically. No secret value enters management facts.
// Migration bundle copies consume pre-reserved slots and explicit backends,
// preserve full refresh/routing metadata, and retain source material until the
// application commits and successfully reopens the replacement state.
// Migration retirement verifies the exact destination owner/backend and complete
// execution material before deleting a provider source. The migration owner must
// exclude retained consumers and concurrent provider writers; the adapter cannot
// infer reference reachability. File verification and removal share one lock.
//
// Materialized secret persistence uses the portable private file store by
// default. The OS keyring is explicit because it is not reliably available on
// headless servers, containers, or WSL. Once created, `secret:` resolves only
// through the keyring and `secretfile:` resolves only through the file store;
// reference authority never falls through to another backend.
package credentials
