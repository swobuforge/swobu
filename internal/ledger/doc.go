// Package ledger defines the M5 durable execution-history application
// boundary. Its Store implementations persist immutable CompletedExchange
// projections; they never participate in routing
// or execution decisions.
//
// The package owns portable history, retention, and health vocabulary.
// Concrete SQLite and PostgreSQL adapters own physical schema, migration, and
// database behavior. Database types and backend-specific configuration must not
// leak through this boundary.
package ledger
