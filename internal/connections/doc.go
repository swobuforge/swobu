// Package connections owns reusable external account/server identities for
// Models, MCP and Search. Sealed specs retain provider validation and credential
// references; Connection values carry stable identity and monotonic revisions.
// MCP specs retain operator-declared execution identity and permission summary.
// These participate in revisioned configuration and survive credential rotation;
// they never prove verified identity or tool safety. Mount admission owns consent.
// Search endpoints reject URL query parameters and fragments at authoring and
// reopen; credentials are independent references, not URL tokens.
// Shared reference grammar and generation belong to domain/connectionidentity,
// below both this registry and routing; no provider configuration lives there.
// The private codec validates complete Connection documents; the Node file
// publishes configuration before the Store exposes it in memory. Checks are separate ephemeral, revision-bound observations and
// cannot advance configuration or establish perpetual readiness.
// ValidateDocument shares the inline decoder without file I/O so
// callers can validate a complete registry before publication. Existing
// Connections change only through revisioned lifecycle operations, and stored
// credentials remain owned by their exact Connection identity.
//
// Lifecycle coordinates reference-safe deletion and reference admission under
// routing -> Connection locks. Routing publication holds that same registry
// read lock until the Node file commit; every primary/fallback Search reference
// is admitted and protects against deletion. Reviewed MCP mounts also check the
// exact approved Connection revision there, without a second source registry.
// Checked creation commits caller-scoped retry receipts and original result
// snapshots in the registry document. Receipts reuse the local management
// 24-hour/4096-entry envelope; expired/oldest receipts leave only on a successful
// publication. Checked edits reauthorize at publication and retain revisions.
// Checked create/edit/delete facts commit with configuration, contain only actor,
// identity, action, revisions and time, and survive receipt expiry or deletion.
// Internal unchecked publication never invents historical management facts.
// Checked credential binding replacement commits original-result replay and
// sanitized management facts in the same document; replacement never resolves
// material, infers external identity or retires a locator.
// Every binding-removing publication (edit, delete or replacement) also records
// old owned slots for eventual reference-safe cleanup when imported state
// predates staging metadata, in the same atomic document publication.
// Owned-slot retirement scans the registry under Workspace publication
// locks, persists a marker before storage effects, and rejects future rebinding
// at registry publication; Models targets carry IDs only. Markers survive storage failure/restart; retry
// snapshots are history, never live credential users.
// Staging records generated backend candidates before storage, without secret
// values or secret hashes. Caller-scoped staging retries reserve the same
// candidates durably and distinguish fresh allocation from uncertain recovery;
// only the credential adapter compares material. Expired candidates retire only after live-reference
// exclusion; cleanup failures preserve retry metadata across restart.
// Startup/periodic cleanup, network checks and API wiring are application-owned
// effects. The store does not infer unused status from an unlocked
// snapshot or perform network requests.
package connections
