// Package config owns process startup preferences and pure platform path resolution.
// StartupConfig.Addr is the single local daemon address: the daemon binds to it
// and internal clients derive their HTTP base URL from it. Routing configuration
// Durable workspace file creation and state do not belong in this package.
// LedgerSettings resolves only secret-free backend and retention preferences.
// Bootstrap owns opening outputs and keeps PostgreSQL credentials out of this
// value, runtime status and diagnostics.
// NodeRuntimeRoot and its ownership check keep direct Search evidence beside
// other runtime evidence; mode changes cannot silently abandon retained facts.
package config
