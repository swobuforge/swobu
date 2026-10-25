// Package postgres implements the M5 Ledger Store for one Swobu deployment per
// PostgreSQL database. A fixed session advisory lock is held for the lifetime
// of the sole writer/migrator connection; this adapter does not provide fleet
// or multi-node history semantics.
//
// Reader queries use a separate pool of at most four connections. Retention is
// age-bounded only: PostgreSQL physical storage remains the database operator's
// responsibility, and this package never runs VACUUM FULL or table rewrites.
// Schema v2 adds standalone MCP history with additive transactional migration
// from v1. The same session-owned writer lock protects both history kinds;
// Workspace queries return immutable metadata and age retention prunes both.
// Schema v3 adds exact Connection ID/revision pairs to model invocation history.
// Historical absence remains absence; no labels or current registry lookups
// fabricate identities. Both columns and marker publish transactionally.
package postgres
