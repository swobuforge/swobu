// Package sqlite implements the M5 Ledger Store using a private, config-scoped
// SQLite database. It owns SQLite schema v3, migrations, PRAGMA enforcement,
// physical retention, and reconstruction of portable ledger values.
// SQLite file URIs normalize native paths so Windows drive letters remain
// path components rather than URI authorities.
//
// The adapter uses WAL with synchronous=NORMAL. Successfully committed history
// survives ordinary process restart, but this is not an audit-grade promise
// across operating-system crash or power loss. Future destructive migrations
// require an explicit backup/reversibility design before this package applies
// them.
// Schema v2 adds standalone MCP operations through an additive transactional
// v1 migration. Age and byte-budget retention include both history kinds.
// Once physical bytes exceed the soft high watermark, writer-owned maintenance
// pressure persists across bounded cycles until 85 percent or no detail remains.
// This transient hysteresis is not durable history or spend authority; restart
// re-evaluates pressure against the high watermark without deleting below it.
// Schema v3 adds exact Connection ID/revision pairs to model invocation history.
// Historical absence remains absence; no labels or current registry lookups
// fabricate identities. Both columns and marker publish transactionally.
package sqlite
