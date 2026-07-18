// Package config owns process startup preferences and pure platform path resolution.
// StartupConfig.Addr is the single local daemon address: the daemon binds to it
// and internal clients derive their HTTP base URL from it. Routing configuration
// Durable workspace file creation and state do not belong in this package.
package config
