// Package postgres implements the M5 Ledger Store for one Swobu deployment per
// PostgreSQL database. A fixed session advisory lock is held for the lifetime
// of the sole writer/migrator connection; this adapter does not provide fleet
// or multi-node history semantics.
//
// Reader queries use a separate pool of at most four connections. Retention is
// age-bounded only: PostgreSQL physical storage remains the database operator's
// responsibility, and this package never runs VACUUM FULL or table rewrites.
package postgres
