// Package ledger defines the M5 durable execution-history application
// boundary. Its Store implementations persist immutable CompletedExchange
// projections; they never participate in routing
// or execution decisions.
//
// The package owns portable history, retention, and health vocabulary.
// Concrete SQLite and PostgreSQL adapters own physical schema, migration, and
// database behavior. Database types and backend-specific configuration must not
// leak through this boundary.
// MCPHistoryStore persists completed standalone calls without fabricating an
// Exchange. Workspace-scoped queries return immutable content-free facts.
// This extension does not by itself establish daemon capture or backend parity.
// One bounded Writer queue carries both completed history kinds. Each kind has
// separate offered/committed/dropped counters; a recovered backend cannot erase
// a known capture gap. Mixed flushes retain per-kind commit state on retry and
// shutdown, since separate Store transactions are not one atomic mixed commit.
// MCP history queries use a zero Workspace slug for the admitted Node-wide
// view; a nonzero slug filters exactly. This is query scope, not authorization.
// Retained MCP pages use exclusive settlement-time/operation-ID cursors and the
// same bounded page sizes as Exchanges. Cursor rows need not remain retained;
// neither exhaustion nor pagination implies complete capture or live history.
// Exact MCP lookup reconstructs one retained standalone operation without a
// bounded-head scan or an invented Exchange. Missing retained evidence is not
// proof that an operation never occurred; callers own admission and scope.
// ActivityPageQuery carries existing ExchangeFilter predicates into indexed
// retained reads. Settlement ranges, exact access identity and delivery result
// are Exchange-only; the HTTP boundary rejects them for MCP. Export presentation
// and download resource bounds belong to the local HTTP adapter, not this store.
package ledger
