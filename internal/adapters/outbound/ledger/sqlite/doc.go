// Package sqlite implements the M5 Ledger Store using a private, config-scoped
// SQLite database. It owns SQLite schema v1, migrations, PRAGMA enforcement,
// physical retention, and reconstruction of portable ledger values.
//
// The adapter uses WAL with synchronous=NORMAL. Successfully committed history
// survives ordinary process restart, but this is not an audit-grade promise
// across operating-system crash or power loss. Future destructive migrations
// require an explicit backup/reversibility design before this package applies
// them.
package sqlite
