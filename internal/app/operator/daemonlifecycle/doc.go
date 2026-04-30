// Package daemonlifecycle owns operator-facing local daemon lifecycle
// capabilities shared across CLI and cockpit adapters.
//
// It centralizes attach-or-start readiness behavior, machine status probing,
// graceful shutdown, startup event emission, and explicit restart orchestration
// so adapters do not duplicate process/shell logic.
package daemonlifecycle
